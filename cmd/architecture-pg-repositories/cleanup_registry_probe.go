package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var cleanupRegistrySamples []map[string]any

type registryProbeSource struct {
	source contracts.CleanupModuleSource
	reads  []map[string]any
}

func (p *registryProbeSource) ResolveCleanupModule(c context.Context, s contracts.AccessScope, op contracts.CleanupOperation) (contracts.CleanupModuleBinding, error) {
	b, e := p.source.ResolveCleanupModule(c, s, op)
	if e == nil {
		p.reads = append(p.reads, map[string]any{"scope": s, "operation": op, "binding": b})
	}
	return b, e
}

func verifyCleanupRegistryPhase(db *sql.DB, profile contracts.Digest, auth contracts.Authenticator, credential contracts.Credential) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	positive, e := seedQueueDeletion(ctx, db, profile, "registry-positive", true)
	if e != nil {
		return e
	}
	revoked, e := seedQueueDeletion(ctx, db, profile, "registry-revoked", true)
	if e != nil {
		return e
	}
	var authority *cleanup.WorkerAuthority
	tasks := postgres.CleanupTasks{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error { return authority.VerifyScope(c, s) }}
	authority, e = cleanup.NewWorkerAuthority(ctx, cleanup.WorkerAuthorityConfig{Credential: credential, Auth: auth, Authorizer: postgres.Authorizer{DB: db, ScopeLifetime: time.Minute}, WorkerID: "cleanup-service", SpaceID: "space", KnowledgeBaseID: "delete-queue-kb", VerifyOperation: tasks.VerifyDeletionOperation})
	if e != nil {
		return e
	}
	defer authority.Close()
	scope, e := authority.Scope(ctx)
	if e != nil {
		return e
	}
	backend := postgres.IndexCleanup{DB: db, Verify: authority.VerifyScope}
	factory := adapters.IndexStoreFactory("reference.cleanup.postgres", backend, backend, db.PingContext, true)
	// Independent host assembly: never take these module IDs/provider/settings
	// from the incoming operation. The default intentionally points elsewhere.
	plan := kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "registry-positive-engine", Provider: "reference.cleanup.postgres", ImplementationVersion: "1.0.0", Settings: json.RawMessage(`{}`)}, {ID: "registry-revoked-engine", Provider: "reference.cleanup.postgres", ImplementationVersion: "1.0.0", Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.IndexCleanupCapability: "registry-revoked-engine"}}
	named := []contracts.ModuleBinding{}
	for _, id := range []contracts.ID{"registry-positive-engine", "registry-revoked-engine"} {
		named = append(named, contracts.ModuleBinding{InstanceID: id, Capability: contracts.IndexCleanupCapability, ContractVersion: "1.0", RequiredFeatures: []string{"generation.delete", "generation.inspect", "delete.fence", "fence.durable"}})
	}
	runtime, e := kernel.BuildBound(ctx, plan, []kernel.Factory{factory}, named)
	if e != nil {
		return e
	}
	defer runtime.Close(ctx)
	identity := runtime.Snapshot()
	digest := contracts.Digest(identity.ConfigVersion)
	activate := func(version int, mode string) error {
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.cleanup_module_snapshots VALUES('space','delete-queue-kb',$1,false)`, version); e != nil {
			return e
		}
		if mode != "empty" {
			for _, id := range []string{"registry-positive", "registry-revoked"} {
				impl, config, profilePin := "1.0.0", string(digest), strings.Repeat("a", 64)
				if mode == "wrong-implementation" {
					impl = "2.0.0"
				}
				if mode == "unknown-runtime" {
					config = strings.Repeat("f", 64)
				}
				if mode == "wrong-profile" {
					profilePin = strings.Repeat("b", 64)
				}
				if _, e = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.cleanup_module_bindings VALUES('space','delete-queue-kb',$1,$2::text||'-binding',$2::text||'-engine',$3,$4,'reference.cleanup.postgres',$5,'1.0',$2::text||'-generation',$2::text||'-epoch','keyword')`, version, id, profilePin, config, impl); e != nil {
					return e
				}
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.cleanup_module_snapshots SET sealed=true WHERE space_id='space' AND kb_id='delete-queue-kb' AND version=$1`, version); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.cleanup_module_heads VALUES('space','delete-queue-kb',$1) ON CONFLICT(space_id,kb_id) DO UPDATE SET version=excluded.version`, version); e != nil {
			return e
		}
		return tx.Commit()
	}
	if e = activate(1, "correct"); e != nil {
		return e
	}
	source := &registryProbeSource{source: postgres.CleanupModules{DB: db, Verify: authority.VerifyScope, VerifyOperation: tasks.VerifyDeletionOperation}}
	buildRegistry := func(r *kernel.Runtime) (*adapters.CleanupRegistry, error) {
		return adapters.NewCleanupRegistry(adapters.CleanupRegistryConfig{Source: source, Scope: authority.CurrentScope, AuthorizeIndex: authority.IndexRequest, Runtimes: map[contracts.Digest]*kernel.Runtime{digest: r}})
	}
	registry, e := buildRegistry(runtime)
	if e != nil {
		return e
	}
	jobs := postgres.DeletionCleanupJobs{DB: db, Verify: authority.VerifyScope, AuthorizeJob: authority.AuthorizeJob}
	request := contracts.DeletionCleanupRequest{Request: contracts.RequestContext{RequestID: "registry-revoked", Deadline: deadline, ProfileDigest: profile}, Scope: scope, JobID: revoked.JobID, WorkerID: "cleanup-service", TargetLeaseDuration: 4 * time.Second}
	selection, e := jobs.NextDeletionCleanup(ctx, request)
	if e != nil || !selection.HasTarget {
		return errors.New("registry negative fixture has no target")
	}
	lease, e := tasks.Claim(ctx, scope, selection.PlanID, selection.TargetID, "cleanup-service", 4*time.Second)
	if e != nil {
		return e
	}
	op, e := cleanup.Operation(lease)
	if e != nil {
		return e
	}
	executor, e := registry.Resolve(ctx, op)
	if e != nil {
		return e
	}
	before, e := executor.Lookup(ctx, op)
	if e != nil || before.State != "absent" {
		return errors.New("registered known generation lookup failed")
	}
	if e = activate(2, "empty"); e != nil {
		return e
	}
	if _, e = executor.Execute(ctx, op); e == nil {
		return errors.New("escaped executor ignored removed deployment binding")
	}
	for version, mode := range []string{"wrong-implementation", "unknown-runtime", "wrong-profile"} {
		if e = activate(version+3, mode); e != nil {
			return e
		}
		if _, e = registry.Resolve(ctx, op); e == nil {
			return errors.New("registry admitted " + mode)
		}
	}
	if e = activate(6, "correct"); e != nil {
		return e
	}
	current, e := registry.Resolve(ctx, op)
	if e != nil {
		return e
	}
	if _, e = current.Lookup(ctx, op); e != nil {
		return e
	}
	if e = runtime.Close(ctx); e != nil {
		return e
	}
	if _, e = source.ResolveCleanupModule(ctx, scope, op); e != nil {
		return errors.New("closed-runtime fixture lost active binding/lease")
	}
	if _, e = current.Lookup(ctx, op); e == nil {
		return errors.New("escaped executor used closed named runtime")
	}
	if e = tasks.Finish(ctx, scope, lease, "unknown", ""); e != nil {
		return e
	}
	failed, e := jobs.ReconcileDeletionCleanup(ctx, request)
	if e != nil || failed.Job.State != "failed" {
		return errors.New("revoked registry target not isolated")
	}
	// Restart an identical host configuration. Old references remain closed.
	replacement, e := kernel.BuildBound(ctx, plan, []kernel.Factory{factory}, named)
	if e != nil {
		return e
	}
	defer replacement.Close(ctx)
	if replacement.Snapshot().ConfigVersion != string(digest) {
		return errors.New("identical host config changed runtime identity")
	}
	registry, e = buildRegistry(replacement)
	if e != nil {
		return e
	}
	target := cleanup.Worker{Tasks: tasks, VerifyLease: tasks.VerifyLease, Resolve: registry.Resolve, AuthorizeExecution: func(c context.Context, s contracts.AccessScope, l contracts.CleanupLease, op contracts.CleanupOperation) error {
		if tasks.VerifyLease(c, s, l) != nil {
			return errors.New("registered worker lost lease")
		}
		if tasks.VerifyDeletionOperation(c, s, op) != nil {
			return errors.New("registered worker operation denied")
		}
		_, e := registry.Resolve(c, op)
		return e
	}, VerifyReceipt: registry.VerifyReceipt}
	queue := postgres.DeletionCleanupQueue{DB: db, Verify: authority.VerifyScope, AuthorizeJob: authority.AuthorizeJob}
	poller, e := cleanup.NewDeletionPoller(cleanup.DeletionPollConfig{Queue: queue, Runner: cleanup.DeletionWorker{Jobs: jobs, Targets: target}, Scope: authority.Scope, WorkerID: "cleanup-service", SpaceID: "space", KnowledgeBaseID: "delete-queue-kb", PageSize: 10, MaxPages: 1, CycleBudget: 10 * time.Second, PollInterval: 10 * time.Millisecond, TargetLeaseDuration: 4 * time.Second})
	if e != nil {
		return e
	}
	cycle, e := poller.RunOnce(ctx)
	if e != nil || cycle.Completed != 1 || cycle.Attempted != 1 {
		return errors.New("registered actual PG consumer failed")
	}
	constraint := func(err error) bool {
		var v interface{ SQLState() string }
		return errors.As(err, &v) && v.SQLState() == "23514"
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.cleanup_module_bindings SET implementation_version='tampered' WHERE space_id='space' AND kb_id='delete-queue-kb' AND version=6`); !constraint(e) {
		return errors.New("sealed cleanup bindings mutable")
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.cleanup_module_heads SET version=1 WHERE space_id='space' AND kb_id='delete-queue-kb'`); !constraint(e) {
		return errors.New("cleanup deployment head regressed")
	}
	if _, e = db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.cleanup_module_snapshots VALUES('space','delete-queue-kb',7,false)`); e != nil {
		return e
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.cleanup_module_heads SET version=7 WHERE space_id='space' AND kb_id='delete-queue-kb'`); !constraint(e) {
		return errors.New("unsealed cleanup deployment activated")
	}
	var bindings, snapshots, head, physical []byte
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(b) ORDER BY version,binding_reference) FROM akos_withdraw_v1.cleanup_module_bindings b`).Scan(&bindings); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(s) ORDER BY version) FROM akos_withdraw_v1.cleanup_module_snapshots s`).Scan(&snapshots); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT row_to_json(h) FROM akos_withdraw_v1.cleanup_module_heads h WHERE space_id='space' AND kb_id='delete-queue-kb'`).Scan(&head); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(jsonb_build_object('job',to_jsonb(j),'runtime',to_jsonb(p),'member',to_jsonb(m),'target',to_jsonb(t),'task',to_jsonb(x),'generation',to_jsonb(g),'deleted',EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id),'remaining_rows',(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=g.id)) ORDER BY j.id) FROM akos_withdraw_v1.document_delete_jobs j JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id JOIN akos_withdraw_v1.document_delete_job_targets m ON m.job_id=j.id JOIN akos_withdraw_v1.cleanup_targets t ON t.plan_id=m.plan_id AND t.id=m.target_id JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id JOIN akos_withdraw_v1.index_generations g ON g.id=t.generation_id WHERE j.id IN($1,$2)`, positive.JobID, revoked.JobID).Scan(&physical); e != nil {
		return e
	}
	cleanupRegistrySamples = append(cleanupRegistrySamples, map[string]any{"positive": positive, "revoked": revoked, "failed": failed, "cycle": cycle, "host_plan": plan, "runtime_identity": identity, "named_bindings": named, "bindings": json.RawMessage(bindings), "snapshots": json.RawMessage(snapshots), "head": json.RawMessage(head), "physical": json.RawMessage(physical), "reads": source.reads, "negative_operation": op, "before": before, "removed_binding_denied": true, "wrong_implementation_denied": true, "unknown_runtime_denied": true, "wrong_profile_denied": true, "closed_runtime_denied": true, "sealed_mutation_denied": true, "head_regression_denied": true, "unsealed_activation_denied": true, "source_authority": "fixture-metadata-and-governance", "deployment_provisioning": "host-plan-and-test-operator-catalog", "worker_authority": "actual-rs256-service-and-sealed-pg-grants"})
	return nil
}
