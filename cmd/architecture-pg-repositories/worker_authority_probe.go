package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/identitytoken"
	"github.com/MemoryGet/memorys/pkg/postgres"
	jose "github.com/go-jose/go-jose/v4"
)

var workerAuthoritySamples []map[string]any

type workerProbeAuth struct {
	source    contracts.Authenticator
	successes int
}

func (p *workerProbeAuth) Authenticate(c context.Context, q contracts.Credential) (contracts.Principal, error) {
	who, e := p.source.Authenticate(c, q)
	if e == nil {
		p.successes++
	}
	return who, e
}

type workerProbeAuthorization struct {
	source    contracts.Authorizer
	decisions []map[string]any
}

func (p *workerProbeAuthorization) Authorize(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	s, e := p.source.Authorize(c, q)
	if e == nil {
		p.decisions = append(p.decisions, map[string]any{"request": q, "scope": s})
	}
	return s, e
}

func nativeWorkerIdentity(ctx context.Context, db *sql.DB) (contracts.Authenticator, contracts.Credential, error) {
	var empty contracts.Credential
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return nil, empty, e
	}
	der, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		return nil, empty, e
	}
	sum := sha256.Sum256(der)
	now := time.Now().Unix()
	issued, expires := now-1, now+120
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO akos_withdraw_v1.identity_issuers VALUES('worker-issuer',true)`, nil},
		{`INSERT INTO akos_withdraw_v1.identity_signing_keys VALUES('worker-issuer','worker-key',$1,true,$2,$3)`, []any{hex.EncodeToString(sum[:]), now - 10, now + 300}},
		{`INSERT INTO akos_withdraw_v1.identity_subjects VALUES('worker-issuer','cleanup-service','service',true)`, nil},
		{`INSERT INTO akos_withdraw_v1.identity_clients VALUES('worker-issuer','cleanup-client',true)`, nil},
		{`INSERT INTO akos_withdraw_v1.identity_tokens VALUES('worker-issuer','cleanup-token','worker-key','cleanup-service','cleanup-client',$1,$2,false)`, []any{issued, expires}},
	}
	for _, s := range statements {
		if _, e = db.ExecContext(ctx, s.sql, s.args...); e != nil {
			return nil, empty, e
		}
	}
	signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("at+jwt").WithHeader("kid", "worker-key"))
	if e != nil {
		return nil, empty, e
	}
	body, _ := json.Marshal(map[string]any{"iss": "worker-issuer", "sub": "cleanup-service", "client_id": "cleanup-client", "aud": "knowledge-worker", "jti": "cleanup-token", "iat": issued, "nbf": issued, "exp": expires})
	signed, e := signer.Sign(body)
	if e != nil {
		return nil, empty, e
	}
	token, e := signed.CompactSerialize()
	if e != nil {
		return nil, empty, e
	}
	auth, e := identitytoken.New(identitytoken.Config{Issuer: "worker-issuer", Audience: "knowledge-worker", SubjectType: "service", MaxLifetime: 5 * time.Minute, Keys: map[string]*rsa.PublicKey{"worker-key": &key.PublicKey}}, postgres.IdentityStatus{DB: db})
	return auth, contracts.Credential{Kind: "access_token", Secret: []byte(token)}, e
}

func verifyWorkerAuthorityPhase(db *sql.DB, profile contracts.Digest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	rawAuth, credential, e := nativeWorkerIdentity(ctx, db)
	if e != nil {
		return e
	}
	defer clear(credential.Secret)
	auth := &workerProbeAuth{source: rawAuth}
	authorizer := &workerProbeAuthorization{source: postgres.Authorizer{DB: db, ScopeLifetime: time.Minute}}
	activate := func(version uint64, actions []string) error {
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES('space',$1,false)`, version); e != nil {
			return e
		}
		for _, action := range actions {
			if _, e = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_grants VALUES('space',$1,'worker-issuer','cleanup-service','cleanup-client','delete-queue-kb',$2)`, version, action); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id='space' AND version=$1`, version); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.access_policy_heads SET version=$1 WHERE space_id='space'`, version); e != nil {
			return e
		}
		return tx.Commit()
	}
	if e = activate(9, []string{"publication.cleanup.execute"}); e != nil {
		return e
	}
	positive, e := seedQueueDeletion(ctx, db, profile, "worker-positive", true)
	if e != nil {
		return e
	}
	denied, e := seedQueueDeletion(ctx, db, profile, "worker-index-denied", true)
	if e != nil {
		return e
	}
	var authority *cleanup.WorkerAuthority
	admissionTasks := postgres.CleanupTasks{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error { return authority.VerifyScope(c, s) }}
	authority, e = cleanup.NewWorkerAuthority(ctx, cleanup.WorkerAuthorityConfig{Credential: credential, Auth: auth, Authorizer: authorizer, WorkerID: "cleanup-service", SpaceID: "space", KnowledgeBaseID: "delete-queue-kb", VerifyOperation: admissionTasks.VerifyDeletionOperation})
	if e != nil {
		return e
	}
	defer authority.Close()
	scope9, e := authority.Scope(ctx)
	if e != nil {
		return e
	}
	forged := scope9
	forged.Action = "index.delete"
	if authority.VerifyScope(ctx, forged) == nil {
		return errors.New("native worker accepted relabeled index scope")
	}
	jobs := postgres.DeletionCleanupJobs{DB: db, Verify: authority.VerifyScope, AuthorizeJob: authority.AuthorizeJob}
	tasks := postgres.CleanupTasks{DB: db, Verify: authority.VerifyScope}
	backend := postgres.IndexCleanup{DB: db, Verify: authority.VerifyScope}
	var operations []contracts.CleanupOperation
	adapter := func(op contracts.CleanupOperation) adapters.IndexCleanup {
		return adapters.IndexCleanup{Index: backend, ModuleInstanceID: op.Target.ModuleInstanceID, ProfileDigest: op.Target.ProfileDigest, Authorize: authority.IndexRequest}
	}
	target := cleanup.Worker{Tasks: tasks, VerifyLease: tasks.VerifyLease, Resolve: func(_ context.Context, op contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
		operations = append(operations, op)
		return adapter(op), nil
	}, AuthorizeExecution: func(c context.Context, s contracts.AccessScope, l contracts.CleanupLease, op contracts.CleanupOperation) error {
		if tasks.VerifyLease(c, s, l) != nil {
			return errors.New("native worker lost lease")
		}
		return tasks.VerifyDeletionOperation(c, s, op)
	}, VerifyReceipt: func(c context.Context, op contracts.CleanupOperation, r contracts.CleanupReceipt) error {
		return adapter(op).VerifyReceipt(c, op, r)
	}}
	worker := cleanup.DeletionWorker{Jobs: jobs, Targets: target}
	request := contracts.DeletionCleanupRequest{Request: contracts.RequestContext{RequestID: "worker-index-denied", Deadline: deadline, ProfileDigest: profile}, Scope: scope9, JobID: denied.JobID, WorkerID: "cleanup-service", TargetLeaseDuration: 4 * time.Second}
	failed, e := worker.RunOne(ctx, request)
	if !errors.Is(e, cleanup.ErrUnknown) || failed.Job.State != "failed" {
		return errors.New("native missing independent index grant did not fail safely")
	}
	if len(operations) != 1 || tasks.VerifyDeletionOperation(ctx, scope9, operations[0]) == nil {
		return errors.New("native operation remained admitted after target ended")
	}
	if e = activate(10, []string{"publication.cleanup.execute", "index.delete"}); e != nil {
		return e
	}
	if authority.VerifyScope(ctx, scope9) == nil {
		return errors.New("native old worker policy accepted")
	}
	scope10, e := authority.Scope(ctx)
	if e != nil {
		return e
	}
	queue := postgres.DeletionCleanupQueue{DB: db, Verify: authority.VerifyScope, AuthorizeJob: authority.AuthorizeJob}
	discovery := contracts.DeletionQueueRequest{Request: contracts.RequestContext{RequestID: "worker-discover", Deadline: deadline}, Scope: scope10, WorkerID: "cleanup-service", Limit: 10}
	for _, change := range []struct{ disable, restore string }{
		{`UPDATE akos_withdraw_v1.identity_tokens SET revoked=true WHERE issuer='worker-issuer'`, `UPDATE akos_withdraw_v1.identity_tokens SET revoked=false WHERE issuer='worker-issuer'`},
		{`UPDATE akos_withdraw_v1.identity_clients SET enabled=false WHERE issuer='worker-issuer'`, `UPDATE akos_withdraw_v1.identity_clients SET enabled=true WHERE issuer='worker-issuer'`},
		{`UPDATE akos_withdraw_v1.identity_signing_keys SET enabled=false WHERE issuer='worker-issuer'`, `UPDATE akos_withdraw_v1.identity_signing_keys SET enabled=true WHERE issuer='worker-issuer'`},
	} {
		if _, e = db.ExecContext(ctx, change.disable); e != nil {
			return e
		}
		if page, err := queue.DiscoverDeletionCleanup(ctx, discovery); err == nil || !reflect.DeepEqual(page, contracts.DeletionQueuePage{}) {
			return errors.New("native worker discovered with revoked identity")
		}
		if _, e = db.ExecContext(ctx, change.restore); e != nil {
			return e
		}
	}
	foreign := discovery
	foreign.Scope.KnowledgeBaseIDs = []contracts.ID{"kb"}
	if _, e = queue.DiscoverDeletionCleanup(ctx, foreign); e == nil {
		return errors.New("native worker crossed scoped KB")
	}
	revokeOnJob := queue
	revokeOnJob.AuthorizeJob = func(c context.Context, s contracts.AccessScope, r contracts.ResourceJob) error {
		if e := authority.AuthorizeJob(c, s, r); e != nil {
			return e
		}
		return activate(11, []string{"index.delete"})
	}
	if page, err := revokeOnJob.DiscoverDeletionCleanup(ctx, discovery); err == nil || !reflect.DeepEqual(page, contracts.DeletionQueuePage{}) {
		return errors.New("native discovery emitted after policy revoked during read")
	}
	if e = activate(12, []string{"publication.cleanup.execute", "index.delete"}); e != nil {
		return e
	}
	// The same production control feeds fresh cycle scopes and all PG boundary
	// verifiers. No callback below invents a permission decision.
	poller, e := cleanup.NewDeletionPoller(cleanup.DeletionPollConfig{Queue: queue, Runner: worker, Scope: authority.Scope, WorkerID: "cleanup-service", SpaceID: "space", KnowledgeBaseID: "delete-queue-kb", PageSize: 10, MaxPages: 1, CycleBudget: 10 * time.Second, PollInterval: 10 * time.Millisecond, TargetLeaseDuration: 4 * time.Second})
	if e != nil {
		return e
	}
	cycle, e := poller.RunOnce(ctx)
	if e != nil || cycle.Completed != 1 || cycle.Attempted != 1 {
		return errors.New("native authenticated worker failed positive cleanup")
	}
	last, e := authority.Scope(ctx)
	if e != nil {
		return e
	}
	discovery.Scope = last
	terminal, e := queue.DiscoverDeletionCleanup(ctx, discovery)
	if e != nil || len(terminal.Items) != 0 {
		return errors.New("native worker rediscovered terminal tasks")
	}
	request.Scope = last
	replay, e := worker.RunOne(ctx, request)
	if e != nil || !reflect.DeepEqual(replay, failed) {
		return errors.New("native worker retried denied task")
	}
	var physical, identity, grants, heads []byte
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(jsonb_build_object('definition',to_jsonb(j),'runtime',to_jsonb(p),'member',to_jsonb(m),'target',to_jsonb(t),'task',to_jsonb(x),'generation',to_jsonb(g),'deleted',EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id),'remaining_rows',(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=g.id)) ORDER BY j.id) FROM akos_withdraw_v1.document_delete_jobs j JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id JOIN akos_withdraw_v1.document_delete_job_targets m ON m.job_id=j.id JOIN akos_withdraw_v1.cleanup_targets t ON t.plan_id=m.plan_id AND t.id=m.target_id JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id JOIN akos_withdraw_v1.index_generations g ON g.id=t.generation_id WHERE j.id IN($1,$2)`, positive.JobID, denied.JobID).Scan(&physical); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT jsonb_build_object('issuer',to_jsonb(i),'subject',to_jsonb(s),'client',to_jsonb(c),'key',to_jsonb(k),'token',to_jsonb(t)) FROM akos_withdraw_v1.identity_issuers i JOIN akos_withdraw_v1.identity_subjects s USING(issuer) JOIN akos_withdraw_v1.identity_clients c USING(issuer) JOIN akos_withdraw_v1.identity_signing_keys k USING(issuer) JOIN akos_withdraw_v1.identity_tokens t USING(issuer) WHERE i.issuer='worker-issuer'`).Scan(&identity); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(g) ORDER BY version,action) FROM akos_withdraw_v1.access_policy_grants g WHERE issuer='worker-issuer'`).Scan(&grants); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(s) ORDER BY version) FROM akos_withdraw_v1.access_policy_snapshots s WHERE space_id='space' AND version BETWEEN 9 AND 12`).Scan(&heads); e != nil {
		return e
	}
	authority.Close()
	if authority.VerifyScope(ctx, last) == nil {
		return errors.New("native closed worker authority reopened")
	}
	workerAuthoritySamples = append(workerAuthoritySamples, map[string]any{"positive": positive, "denied": denied, "failed": failed, "failed_replay": replay, "cycle": cycle, "scope9": scope9, "scope10": scope10, "scope12": last, "identity": json.RawMessage(identity), "grants": json.RawMessage(grants), "policy_snapshots": json.RawMessage(heads), "physical": json.RawMessage(physical), "operations": operations, "decisions": authorizer.decisions, "successful_authentications": auth.successes, "terminal": terminal, "token_client_key_revocation_denied": true, "revoked_during_discovery_denied": true, "stale_policy_denied": true, "relabel_denied": true, "finished_operation_denied": true, "cross_kb_denied": true, "closed_denied": true, "worker_authority": "actual-rs256-service-and-sealed-pg-grants", "source_authority": "fixture-metadata-and-governance", "module_resolution": "in-process-pg-index-adapter-no-production-registry"})
	return verifyCleanupRegistryPhase(db, profile, rawAuth, credential)
}
