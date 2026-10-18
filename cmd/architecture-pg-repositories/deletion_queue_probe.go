package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var deletionQueueSamples []map[string]any

type recordingDeletionQueue struct {
	source contracts.DeletionCleanupQueue
	pages  []map[string]any
}

func (q *recordingDeletionQueue) DiscoverDeletionCleanup(c context.Context, r contracts.DeletionQueueRequest) (contracts.DeletionQueuePage, error) {
	p, e := q.source.DiscoverDeletionCleanup(c, r)
	if e == nil {
		q.pages = append(q.pages, map[string]any{"request": r, "page": p})
	}
	return p, e
}

// Isolated metadata fixtures test discovery/scheduling, not the publication
// chain. The preceding seven-target consumer probe covers actual publication.
func seedQueueDeletion(ctx context.Context, db *sql.DB, profile contracts.Digest, id string, indexed bool) (contracts.DeleteDocumentResult, error) {
	if _, e := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.documents(space_id,kb_id,id,publication_epoch,row_version) VALUES('space','delete-queue-kb',$1::text,1,1)`, id); e != nil {
		return contracts.DeleteDocumentResult{}, e
	}
	version := uint64(1)
	if indexed {
		statements := []string{
			`INSERT INTO akos_withdraw_v1.revisions(space_id,kb_id,document_id,id,content_digest) VALUES('space','delete-queue-kb',$1::text,$1::text||'-revision',repeat('a',64))`,
			`INSERT INTO akos_withdraw_v1.generation_sets VALUES('space','delete-queue-kb',$1::text,$1::text||'-revision',$1::text||'-set',$1::text||'-epoch')`,
			`INSERT INTO akos_withdraw_v1.index_generations(space_id,kb_id,document_id,revision_id,generation_set_id,id,backend_epoch_id,channel,profile_digest,module_instance_id) VALUES('space','delete-queue-kb',$1::text,$1::text||'-revision',$1::text||'-set',$1::text||'-generation',$1::text||'-epoch','keyword',repeat('a',64),$1::text||'-engine')`,
			`INSERT INTO akos_withdraw_v1.binding_definitions VALUES('space','delete-queue-kb',$1::text||'-binding',$1::text||'-engine',repeat('a',64),'index',jsonb_build_object('generation_id',$1::text||'-generation'))`,
			`INSERT INTO akos_withdraw_v1.index_projection_rows VALUES($1::text||'-generation',$1::text||'-chunk','{"text":"queue fixture"}')`,
			`UPDATE akos_withdraw_v1.documents SET active_revision_id=$1::text||'-revision',active_generation_set_id=$1::text||'-set',row_version=2 WHERE id=$1::text AND kb_id='delete-queue-kb'`,
		}
		for _, sql := range statements {
			if _, e := db.ExecContext(ctx, sql, id); e != nil {
				return contracts.DeleteDocumentResult{}, e
			}
		}
		version = 2
	}
	deadline, _ := ctx.Deadline()
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "worker-a" || s.Action != "document.delete" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "delete-queue-kb" {
			return errors.New("queue source fixture denied")
		}
		return nil
	}
	authorize := func(c context.Context, q contracts.DeleteDocumentRequest) error {
		if q.DocumentID != contracts.ID(id) {
			return errors.New("queue fixture resource denied")
		}
		return verify(c, q.Command.Scope)
	}
	request := contracts.DeleteDocumentRequest{DocumentID: contracts.ID(id), ExpectedDocumentVersion: version, Command: contracts.CommandContext{Principal: contracts.Principal{SubjectID: "worker-a", SubjectType: "user", Issuer: "fixture"}, Scope: contracts.AccessScope{ScopeID: "queue-source", SubjectID: "worker-a", Action: "document.delete", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"delete-queue-kb"}, PolicyVersion: 1, ExpiresAt: deadline}, Request: contracts.RequestContext{RequestID: contracts.ID(id), Deadline: deadline, ProfileDigest: profile}, IdempotencyKey: id}}
	return (document.DeletionService{Store: postgres.DocumentDeletions{DB: db, Verify: verify, AuthorizeDelete: authorize}, Verify: authorize}).Delete(ctx, request)
}

func verifyDeletionQueuePhase(db *sql.DB, profile contracts.Digest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var seeds []contracts.DeleteDocumentResult
	for i := 0; i < 4; i++ {
		r, e := seedQueueDeletion(ctx, db, profile, fmt.Sprintf("queue-document-%d", i), i >= 2)
		if e != nil {
			return fmt.Errorf("queue seed: %w", e)
		}
		seeds = append(seeds, r)
	}
	policy := uint64(1)
	revoked := false
	scopeCount := 0
	scope := func() contracts.AccessScope {
		return contracts.AccessScope{ScopeID: contracts.ID(fmt.Sprintf("queue-scope-%d", scopeCount)), SubjectID: "worker-a", Action: "publication.cleanup.execute", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"delete-queue-kb"}, PolicyVersion: policy, ExpiresAt: deadline}
	}
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || revoked || s.SubjectID != "worker-a" || s.Action != "publication.cleanup.execute" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "delete-queue-kb" || s.PolicyVersion != policy || !s.ExpiresAt.After(time.Now()) {
			return errors.New("queue worker fixture denied")
		}
		return nil
	}
	allowed := map[contracts.ID]contracts.ID{}
	for _, s := range seeds {
		allowed[s.JobID] = s.DocumentID
	}
	authorize := func(c context.Context, s contracts.AccessScope, r contracts.ResourceJob) error {
		if verify(c, s) != nil || r.ResourceID != allowed[r.Job.ID] || r.Job.SpaceID != "space" {
			return errors.New("queue job denied")
		}
		return nil
	}
	source := postgres.DeletionCleanupQueue{DB: db, Verify: verify, AuthorizeJob: authorize}
	jobs := postgres.DeletionCleanupJobs{DB: db, Verify: verify, AuthorizeJob: authorize}
	tasks := postgres.CleanupTasks{DB: db, Verify: verify}
	indexVerify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.ScopeID != "queue-index-grant" || s.SubjectID != "worker-a" || s.Action != "index.delete" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "delete-queue-kb" || s.PolicyVersion != policy {
			return errors.New("queue index fixture denied")
		}
		return nil
	}
	backend := postgres.IndexCleanup{DB: db, Verify: indexVerify}
	adapter := func(op contracts.CleanupOperation) adapters.IndexCleanup {
		return adapters.IndexCleanup{Index: backend, ModuleInstanceID: op.Target.ModuleInstanceID, ProfileDigest: op.Target.ProfileDigest, Authorize: func(c context.Context, v contracts.CleanupOperation) (contracts.IndexDeleteRequest, error) {
			if v.DocumentID != seeds[2].DocumentID || v.Target.Kind != "index" || v.Target.GenerationID == nil {
				return contracts.IndexDeleteRequest{}, errors.New("queue index resource denied")
			}
			d, _ := c.Deadline()
			grant := contracts.AccessScope{ScopeID: "queue-index-grant", SubjectID: "worker-a", Action: "index.delete", SpaceID: v.SpaceID, KnowledgeBaseIDs: []contracts.ID{v.KnowledgeBaseID}, PolicyVersion: policy, ExpiresAt: d}
			return contracts.IndexDeleteRequest{Request: contracts.RequestContext{RequestID: v.ID, ProfileDigest: v.Target.ProfileDigest, Deadline: d}, Scope: grant, GenerationID: *v.Target.GenerationID}, nil
		}}
	}
	target := cleanup.Worker{Tasks: tasks, VerifyLease: tasks.VerifyLease, Resolve: func(_ context.Context, op contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
		return adapter(op), nil
	}, AuthorizeExecution: func(c context.Context, s contracts.AccessScope, l contracts.CleanupLease, op contracts.CleanupOperation) error {
		var bound bool
		if verify(c, s) != nil || db.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.document_delete_jobs j ON j.id=m.job_id WHERE m.plan_id=$1 AND m.target_id=$2 AND j.document_id=$3 AND j.kb_id='delete-queue-kb')`, op.PlanID, op.TargetID, l.DocumentID).Scan(&bound) != nil || !bound {
			return errors.New("queue effect denied")
		}
		return nil
	}, VerifyReceipt: func(c context.Context, op contracts.CleanupOperation, r contracts.CleanupReceipt) error {
		return adapter(op).VerifyReceipt(c, op, r)
	}}
	worker := cleanup.DeletionWorker{Jobs: jobs, Targets: target}
	// Establish a genuinely failed task using the ordinary lease/unknown path.
	unavailable := target
	unavailable.Resolve = func(context.Context, contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
		return nil, errors.New("queue fixture backend unavailable")
	}
	failedWorker := cleanup.DeletionWorker{Jobs: jobs, Targets: unavailable}
	failed, e := failedWorker.RunOne(ctx, contracts.DeletionCleanupRequest{Request: contracts.RequestContext{RequestID: "queue-failure", Deadline: deadline, ProfileDigest: profile}, Scope: scope(), JobID: seeds[3].JobID, WorkerID: "worker-a", TargetLeaseDuration: 4 * time.Second})
	if !errors.Is(e, cleanup.ErrUnknown) || failed.Job.State != "failed" {
		return errors.New("queue failed fixture not isolated")
	}
	snapshot := func() (json.RawMessage, error) {
		var raw []byte
		e := db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY p.job_id) FROM akos_withdraw_v1.document_delete_job_progress p JOIN akos_withdraw_v1.document_delete_jobs j ON j.id=p.job_id WHERE j.kb_id<>'delete-queue-kb'`).Scan(&raw)
		return raw, e
	}
	otherBefore, e := snapshot()
	if e != nil {
		return e
	}
	recording := &recordingDeletionQueue{source: source}
	poller, e := cleanup.NewDeletionPoller(cleanup.DeletionPollConfig{Queue: recording, Runner: worker, Scope: func(c context.Context) (contracts.AccessScope, error) {
		scopeCount++
		s := scope()
		return s, verify(c, s)
	}, WorkerID: "worker-a", SpaceID: "space", KnowledgeBaseID: "delete-queue-kb", PageSize: 1, MaxPages: 1, CycleBudget: 3 * time.Second, PollInterval: 10 * time.Millisecond, TargetLeaseDuration: 4 * time.Second})
	if e != nil {
		return e
	}
	var cycles []cleanup.DeletionPollReport
	first, e := poller.RunOnce(ctx)
	if e != nil {
		return e
	}
	cycles = append(cycles, first)
	if len(recording.pages) != 1 {
		return errors.New("queue missing initial page")
	}
	initial := recording.pages[0]["page"].(contracts.DeletionQueuePage)
	if initial.Next == nil {
		return errors.New("queue did not page bounded jobs")
	}
	q := contracts.DeletionQueueRequest{Request: contracts.RequestContext{RequestID: "queue-check", Deadline: deadline}, Scope: scope(), WorkerID: "worker-a", Limit: 1, Cursor: initial.Next}
	policy = 2
	q.Scope = scope()
	if page, err := source.DiscoverDeletionCleanup(ctx, q); err == nil || !reflect.DeepEqual(page, contracts.DeletionQueuePage{}) {
		return errors.New("queue reused stale policy cursor")
	}
	q.Cursor = nil
	q.Scope.KnowledgeBaseIDs = []contracts.ID{"kb"}
	if page, err := source.DiscoverDeletionCleanup(ctx, q); err == nil || len(page.Items) != 0 {
		return errors.New("queue crossed KB authority")
	}
	q.Scope = scope()
	q.Scope.ExpiresAt = time.Now().Add(-time.Second)
	if page, err := source.DiscoverDeletionCleanup(ctx, q); err == nil || len(page.Items) != 0 {
		return errors.New("queue accepted expired authority")
	}
	q.Scope = scope()
	revoked = true
	if page, err := source.DiscoverDeletionCleanup(ctx, q); err == nil || len(page.Items) != 0 {
		return errors.New("queue accepted revoked authority")
	}
	revoked = false
	second, e := poller.RunOnce(ctx)
	if e != nil {
		return e
	}
	cycles = append(cycles, second)
	if recording.pages[1]["request"].(contracts.DeletionQueueRequest).Cursor != nil {
		return errors.New("poller failed to restart after policy change")
	}
	runContext, stop := context.WithCancel(ctx)
	defer stop()
	runError := poller.Run(runContext, func(r cleanup.DeletionPollReport) {
		cycles = append(cycles, r)
		if r.Completed == 1 || len(cycles) > 5 {
			stop()
		}
	})
	if !errors.Is(runError, context.Canceled) || len(cycles) != 3 {
		return fmt.Errorf("queue did not complete/cancel bounded loop: %v", runError)
	}
	q.Scope = scope()
	empty, e := source.DiscoverDeletionCleanup(ctx, q)
	if e != nil || len(empty.Items) != 0 || empty.Next != nil {
		return errors.New("queue rediscovered terminal jobs")
	}
	failedReplay, e := failedWorker.RunOne(ctx, contracts.DeletionCleanupRequest{Request: contracts.RequestContext{RequestID: "queue-no-retry", Deadline: deadline, ProfileDigest: profile}, Scope: scope(), JobID: seeds[3].JobID, WorkerID: "worker-a", TargetLeaseDuration: 4 * time.Second})
	if e != nil || !reflect.DeepEqual(failed, failedReplay) {
		return errors.New("queue failed job retried")
	}
	otherAfter, e := snapshot()
	if e != nil || string(otherBefore) != string(otherAfter) {
		return errors.New("queue touched other KB jobs")
	}
	var runtime, physical []byte
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(jsonb_build_object('definition',to_jsonb(j),'progress',to_jsonb(p)) ORDER BY j.id) FROM akos_withdraw_v1.document_delete_jobs j JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id WHERE j.kb_id='delete-queue-kb'`).Scan(&runtime); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(jsonb_build_object('membership',to_jsonb(m),'target',to_jsonb(t),'progress',to_jsonb(x),'generation',to_jsonb(g),'deleted',EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=t.generation_id),'remaining_rows',(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=t.generation_id)) ORDER BY m.job_id) FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_targets t ON t.plan_id=m.plan_id AND t.id=m.target_id JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id JOIN akos_withdraw_v1.index_generations g ON g.id=t.generation_id WHERE g.kb_id='delete-queue-kb'`).Scan(&physical); e != nil {
		return e
	}
	deletionQueueSamples = append(deletionQueueSamples, map[string]any{"seeds": seeds, "pages": recording.pages, "cycles": cycles, "scope_renewals": scopeCount, "failed": failed, "failed_replay": failedReplay, "terminal_discovery": empty, "runtime": json.RawMessage(runtime), "physical": json.RawMessage(physical), "other_before": otherBefore, "other_after": otherAfter, "stale_policy_cursor_denied": true, "cross_kb_denied": true, "expired_scope_denied": true, "revoked_scope_denied": true, "canceled": errors.Is(runError, context.Canceled), "source_authority": "fixture-metadata-and-governance", "worker_authority": "fixture-independent-task-and-index-grants"})
	return verifyWorkerAuthorityPhase(db, profile)
}
