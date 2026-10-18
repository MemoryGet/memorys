package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var deletionCleanupSamples []map[string]any

func verifyDeletionCleanupPhase(db *sql.DB, auth contracts.Authenticator, viewer contracts.Credential, emptyJob contracts.ID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if _, e := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES('space',8,false); INSERT INTO akos_withdraw_v1.access_policy_grants VALUES('space',8,'fixture','reader','identity-client','kb','resource.view_job'); UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id='space' AND version=8; UPDATE akos_withdraw_v1.access_policy_heads SET version=8 WHERE space_id='space' AND version=7`); e != nil {
		return e
	}
	var jobID contracts.ID
	var profile contracts.Digest
	if e := db.QueryRowContext(ctx, `SELECT j.id,j.profile_digest FROM akos_withdraw_v1.document_delete_jobs j WHERE j.document_id=$1`, historicalTarget.Document).Scan(&jobID, &profile); e != nil {
		return e
	}
	scope := contracts.AccessScope{ScopeID: "delete-job-consumer", SubjectID: "worker-a", Action: "publication.cleanup.execute", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: deadline}
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "worker-a" || s.Action != "publication.cleanup.execute" && s.Action != "publication.cleanup.control" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("delete job worker fixture denied")
		}
		return nil
	}
	allowedJobs := map[contracts.ID]bool{jobID: true, emptyJob: true}
	jobs := postgres.DeletionCleanupJobs{DB: db, Verify: verify, AuthorizeJob: func(c context.Context, s contracts.AccessScope, r contracts.ResourceJob) error {
		if verify(c, s) != nil || r.Job.SpaceID != "space" || !allowedJobs[r.Job.ID] {
			return errors.New("delete job resource fixture denied")
		}
		return nil
	}, AuthorizeControl: func(c context.Context, s contracts.AccessScope, r contracts.ResourceJob) error {
		if c.Err() != nil || s.SubjectID != "worker-a" || s.Action != "publication.cleanup.control" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || r.Job.SpaceID != "space" || !allowedJobs[r.Job.ID] {
			return errors.New("delete job control fixture denied")
		}
		return nil
	}}
	tasks := postgres.CleanupTasks{DB: db, Verify: verify}
	indexVerify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.ScopeID != "delete-job-index" || s.SubjectID != "worker-a" || s.Action != "index.delete" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("delete job index fixture denied")
		}
		return nil
	}
	backend := postgres.IndexCleanup{DB: db, Verify: indexVerify}
	adapter := func(op contracts.CleanupOperation) adapters.IndexCleanup {
		return adapters.IndexCleanup{Index: backend, ModuleInstanceID: op.Target.ModuleInstanceID, ProfileDigest: op.Target.ProfileDigest, Authorize: func(c context.Context, v contracts.CleanupOperation) (contracts.IndexDeleteRequest, error) {
			if v.DocumentID != historicalTarget.Document || v.Target.Kind != "index" || v.Target.GenerationID == nil {
				return contracts.IndexDeleteRequest{}, errors.New("delete job effect fixture denied")
			}
			deadline, _ := c.Deadline()
			grant := contracts.AccessScope{ScopeID: "delete-job-index", SubjectID: "worker-a", Action: "index.delete", SpaceID: v.SpaceID, KnowledgeBaseIDs: []contracts.ID{v.KnowledgeBaseID}, PolicyVersion: 1, ExpiresAt: deadline}
			return contracts.IndexDeleteRequest{Request: contracts.RequestContext{RequestID: v.ID, ProfileDigest: v.Target.ProfileDigest, Deadline: deadline}, Scope: grant, GenerationID: *v.Target.GenerationID}, nil
		}}
	}
	targetWorker := cleanup.Worker{Tasks: tasks, VerifyLease: tasks.VerifyLease, Resolve: func(_ context.Context, op contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
		return adapter(op), nil
	}, AuthorizeExecution: func(c context.Context, s contracts.AccessScope, l contracts.CleanupLease, op contracts.CleanupOperation) error {
		var bound bool
		if verify(c, s) != nil || l.DocumentID != historicalTarget.Document || db.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets WHERE job_id=$1 AND plan_id=$2 AND target_id=$3)`, jobID, op.PlanID, op.TargetID).Scan(&bound) != nil || !bound {
			return errors.New("delete job effect admission denied")
		}
		return nil
	}, VerifyReceipt: func(c context.Context, op contracts.CleanupOperation, r contracts.CleanupReceipt) error {
		return adapter(op).VerifyReceipt(c, op, r)
	}}
	worker := cleanup.DeletionWorker{Jobs: jobs, Targets: targetWorker}
	status := document.JobStatusService{Auth: auth, Reader: postgres.DeletionJobReader{DB: db, Authorizer: postgres.Authorizer{DB: db, ScopeLifetime: time.Minute}}}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/knowledge-os/jobs/{id}", api.JobStatusHandler{Service: status})
	server := httptest.NewServer(mux)
	defer server.Close()
	read := func(id contracts.ID) (json.RawMessage, error) {
		r, e := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/knowledge-os/jobs/"+string(id), nil)
		if e != nil {
			return nil, e
		}
		r.Header.Set("Authorization", "Bearer "+string(viewer.Secret))
		response, e := server.Client().Do(r)
		if e != nil {
			return nil, e
		}
		defer response.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
		if e != nil || response.StatusCode != 200 || len(raw) > 65536 {
			return nil, errors.New("cleanup progress HTTP unavailable")
		}
		return raw, nil
	}
	before, e := read(jobID)
	if e != nil {
		return e
	}
	request := contracts.DeletionCleanupRequest{Request: contracts.RequestContext{RequestID: "delete-job-consume", Deadline: deadline, ProfileDigest: profile}, Scope: scope, JobID: jobID, WorkerID: "worker-a", TargetLeaseDuration: 4 * time.Second}
	if _, e = jobs.NextDeletionCleanup(ctx, request); e != nil {
		return e
	}
	// A lying successful task ledger must not turn into completed physical work.
	var pending, total int
	if e = db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE x.state='pending' AND x.row_version=1 AND x.fencing_token=0) FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=$1`, jobID).Scan(&total, &pending); e != nil || total < 2 || pending != total {
		return errors.New("delete job fixture is not fully pending")
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.cleanup_progress x SET state='succeeded',worker_id='fixture-forged',fencing_token=1,attempt=1,row_version=3,result_reference='fixture-forged-result' WHERE EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets m WHERE m.job_id=$1 AND m.plan_id=x.plan_id AND m.target_id=x.target_id)`, jobID); e != nil {
		return e
	}
	if _, e = jobs.ReconcileDeletionCleanup(ctx, request); e == nil {
		return errors.New("forged task receipts completed delete job")
	}
	constraintRejected := func(err error) bool {
		var state interface{ SQLState() string }
		return errors.As(err, &state) && state.SQLState() == "23514"
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.document_delete_job_progress SET state='completed',completed_targets=total_targets,row_version=row_version+1 WHERE job_id=$1`, jobID); !constraintRejected(e) {
		return errors.New("raw completed state bypassed backend proof")
	}
	// Restore only the deliberate fault injection in this private fixture.
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.cleanup_progress x SET state='pending',worker_id=NULL,fencing_token=0,attempt=0,row_version=1,result_reference=NULL WHERE EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets m WHERE m.job_id=$1 AND m.plan_id=x.plan_id AND m.target_id=x.target_id)`, jobID); e != nil {
		return e
	}
	if _, e = db.ExecContext(ctx, `DELETE FROM akos_withdraw_v1.document_delete_job_targets WHERE (job_id,plan_id,target_id) IN(SELECT job_id,plan_id,target_id FROM akos_withdraw_v1.document_delete_job_targets WHERE job_id=$1 LIMIT 1)`, jobID); !constraintRejected(e) {
		return errors.New("frozen job target membership could be removed")
	}

	var other contracts.ID
	if e = db.QueryRowContext(ctx, `SELECT g.id FROM akos_withdraw_v1.index_generations g WHERE g.document_id<>$1 AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id) AND EXISTS(SELECT 1 FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=g.id) ORDER BY g.id LIMIT 1`, historicalTarget.Document).Scan(&other); e != nil {
		return e
	}
	inspectOther := func() (json.RawMessage, error) {
		var raw []byte
		e := db.QueryRowContext(ctx, `SELECT jsonb_build_object('generation_id',g.id,'document_id',g.document_id,'deleted',EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id),'remaining_rows',(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=g.id)) FROM akos_withdraw_v1.index_generations g WHERE g.id=$1`, other).Scan(&raw)
		return raw, e
	}
	otherBefore, e := inspectOther()
	if e != nil {
		return e
	}
	var steps []map[string]any
	var completed contracts.ResourceJob
	for i := 0; i < total; i++ {
		completed, e = worker.RunOne(ctx, request)
		if e != nil {
			return e
		}
		wire, e := read(jobID)
		if e != nil {
			return e
		}
		steps = append(steps, map[string]any{"record": completed, "http_result": wire})
	}
	if completed.Job.State != "completed" || completed.Job.Progress != 1 {
		return errors.New("delete job did not complete actual targets")
	}
	replay, e := worker.RunOne(ctx, request)
	if e != nil || !reflect.DeepEqual(replay, completed) {
		return errors.New("completed delete job replay changed progress")
	}
	var emptyProfile contracts.Digest
	if e = db.QueryRowContext(ctx, `SELECT profile_digest FROM akos_withdraw_v1.document_delete_jobs WHERE id=$1`, emptyJob).Scan(&emptyProfile); e != nil {
		return e
	}
	emptyRequest := request
	emptyRequest.JobID = emptyJob
	emptyRequest.Request.ProfileDigest = emptyProfile
	empty, e := worker.RunOne(ctx, emptyRequest)
	if e != nil || empty.Job.State != "completed" || empty.Job.Progress != 1 {
		return errors.New("empty delete job failed completion")
	}
	var emptyCount int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.document_delete_job_targets WHERE job_id=$1`, emptyJob).Scan(&emptyCount); e != nil || emptyCount != 0 {
		return errors.New("empty cleanup job has unexpected targets")
	}
	emptyWire, e := read(emptyJob)
	if e != nil {
		return e
	}
	failedSeed, e := seedUnavailableCleanupJob(ctx, db, profile)
	if e != nil {
		return e
	}
	failedJob, failedProfile := failedSeed.JobID, profile
	allowedJobs[failedJob] = true
	failedRequest := request
	failedRequest.JobID = failedJob
	failedRequest.Request.ProfileDigest = failedProfile
	failedRequest.Request.RequestID = "delete-job-backend-unavailable"
	unavailable := targetWorker
	unavailable.Resolve = func(context.Context, contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
		return nil, errors.New("fixture backend unavailable")
	}
	failedWorker := cleanup.DeletionWorker{Jobs: jobs, Targets: unavailable}
	failedRecord, e := failedWorker.RunOne(ctx, failedRequest)
	if !errors.Is(e, cleanup.ErrUnknown) || failedRecord.Job.State != "failed" || failedRecord.Job.Error == nil {
		return errors.New("unknown cleanup effect was not isolated for reconciliation")
	}
	failedWire, e := read(failedJob)
	if e != nil {
		return e
	}
	failedReplay, e := failedWorker.RunOne(ctx, failedRequest)
	if e != nil || !reflect.DeepEqual(failedRecord, failedReplay) {
		return errors.New("failed job was blindly retried")
	}
	controlScope := scope
	controlScope.ScopeID = "delete-job-control"
	controlScope.Action = "publication.cleanup.control"
	controlRequest := contracts.DeletionCleanupControlRequest{Request: contracts.RequestContext{RequestID: "delete-job-retry-unknown", Deadline: deadline, ProfileDigest: failedProfile}, Scope: controlScope, JobID: failedJob, ExpectedJobVersion: failedRecord.Job.RowVersion}
	if _, e = jobs.RetryDeletionCleanup(ctx, controlRequest); e == nil {
		return errors.New("unknown cleanup effect was reset by retry")
	}
	// This private fixture records an independent reconciliation observation:
	// the external owner has established that the failed target had no effect.
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.cleanup_progress x SET state='failed',worker_id='reconciler',lease_until=NULL,result_reference=NULL,row_version=row_version+1 WHERE EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets m WHERE m.job_id=$1 AND m.plan_id=x.plan_id AND m.target_id=x.target_id) AND x.state='unknown'`, failedJob); e != nil {
		return e
	}
	controlRequest.Request.RequestID = "delete-job-retry-known-failure"
	retried, e := jobs.RetryDeletionCleanup(ctx, controlRequest)
	if e != nil || retried.Job.ID != failedJob || retried.Job.State != "running" || retried.Job.Attempt != failedRecord.Job.Attempt+1 || retried.Job.RowVersion != failedRecord.Job.RowVersion+1 {
		return fmt.Errorf("known failed cleanup was not explicitly reset: err=%v result=%+v prior=%+v", e, retried, failedRecord)
	}
	var retryTargetState, retryTargetAttemptText, retryTargetFenceText string
	if e = db.QueryRowContext(ctx, `SELECT x.state,x.attempt::text,x.fencing_token::text FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=$1`, failedJob).Scan(&retryTargetState, &retryTargetAttemptText, &retryTargetFenceText); e != nil {
		return e
	}
	retryTargetAttempt, attemptErr := strconv.ParseUint(retryTargetAttemptText, 10, 64)
	retryTargetFence, fenceErr := strconv.ParseUint(retryTargetFenceText, 10, 64)
	if attemptErr != nil || fenceErr != nil || retryTargetState != "pending" || retryTargetAttempt == 0 || retryTargetFence == 0 {
		return errors.New("retry did not preserve target attempt/fence")
	}
	// A retried job still needs a fresh target execution; an unavailable
	// backend returns to reconciliation instead of being treated as success.
	failedRequest.Request.RequestID = "delete-job-retry-backend-unavailable"
	retriedFailure, e := failedWorker.RunOne(ctx, failedRequest)
	if !errors.Is(e, cleanup.ErrUnknown) || retriedFailure.Job.State != "failed" {
		return errors.New("retried unavailable cleanup escaped reconciliation")
	}
	var failedRuntime, failedTargets []byte
	if e = db.QueryRowContext(ctx, `SELECT row_to_json(p) FROM akos_withdraw_v1.document_delete_job_progress p WHERE job_id=$1`, failedJob).Scan(&failedRuntime); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(x ORDER BY x.plan_id,x.target_id) FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=$1`, failedJob).Scan(&failedTargets); e != nil {
		return e
	}
	var failedIndex []byte
	if e = db.QueryRowContext(ctx, `SELECT jsonb_build_object('generation_id',g.id,'document_id',g.document_id,'deleted',EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id),'remaining_rows',(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=g.id)) FROM akos_withdraw_v1.index_generations g WHERE g.id='delete-failure-generation'`).Scan(&failedIndex); e != nil {
		return e
	}
	original, e := deletedOriginalProbe(ctx)
	if e != nil {
		return e
	}
	otherAfter, e := inspectOther()
	if e != nil || string(otherBefore) != string(otherAfter) {
		return errors.New("delete job touched another source")
	}
	var actual, progress []byte
	if e = db.QueryRowContext(ctx, `SELECT jsonb_agg(jsonb_build_object('membership',to_jsonb(m),'target',to_jsonb(t),'plan',to_jsonb(p),'progress',to_jsonb(x),'generation',to_jsonb(g),'deleted',EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=t.generation_id),'remaining_rows',(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=t.generation_id)) ORDER BY m.plan_id,m.target_id) FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_targets t ON t.plan_id=m.plan_id AND t.id=m.target_id JOIN akos_withdraw_v1.cleanup_plans p ON p.id=m.plan_id JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id JOIN akos_withdraw_v1.index_generations g ON g.id=t.generation_id WHERE m.job_id=$1`, jobID).Scan(&actual); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, `SELECT row_to_json(p) FROM akos_withdraw_v1.document_delete_job_progress p WHERE job_id=$1`, jobID).Scan(&progress); e != nil {
		return e
	}
	var statusGrant []byte
	if e = db.QueryRowContext(ctx, `SELECT row_to_json(g) FROM akos_withdraw_v1.access_policy_grants g WHERE space_id='space' AND version=8 AND subject_id='reader' AND action='resource.view_job'`).Scan(&statusGrant); e != nil {
		return e
	}
	deletionCleanupSamples = append(deletionCleanupSamples, map[string]any{"failure_source_authority": "fixture-metadata-and-governance", "failed_index": json.RawMessage(failedIndex), "status_grant": json.RawMessage(statusGrant), "failed_record": failedRecord, "failed_http": failedWire, "failed_replay": failedReplay, "unknown_retry_rejected": true, "retry_known_failure": retried, "retry_target_state": retryTargetState, "retry_target_attempt": retryTargetAttempt, "retry_target_fencing": retryTargetFence, "retried_failure": retriedFailure, "failed_runtime": json.RawMessage(failedRuntime), "failed_targets": json.RawMessage(failedTargets), "job_id": jobID, "resource_id": historicalTarget.Document, "before": before, "steps": steps, "completed": completed, "replay": replay, "runtime": json.RawMessage(progress), "actual_targets": json.RawMessage(actual), "target_count": total, "empty_target_count": emptyCount, "empty_record": empty, "empty_http": emptyWire, "original": original, "untouched_before": otherBefore, "untouched_after": otherAfter, "forged_success_rejected": true, "raw_completion_rejected": true, "sealed_targets_rejected": true, "worker_authority": "fixture-independent-task-and-index-grants", "status_authority": "actual-rs256-resource.view_job"})
	return verifyDeletionQueuePhase(db, profile)
}

// The negative branch uses explicit source metadata fixture; the successful
// branch above uses the actual eight-epoch publication chain.
func seedUnavailableCleanupJob(ctx context.Context, db *sql.DB, profile contracts.Digest) (contracts.DeleteDocumentResult, error) {
	statements := []string{
		`INSERT INTO akos_withdraw_v1.documents(space_id,kb_id,id,publication_epoch,row_version) VALUES('space','kb','delete-failure-document',1,1)`,
		`INSERT INTO akos_withdraw_v1.revisions(space_id,kb_id,document_id,id,content_digest) VALUES('space','kb','delete-failure-document','delete-failure-revision',repeat('a',64))`,
		`INSERT INTO akos_withdraw_v1.generation_sets VALUES('space','kb','delete-failure-document','delete-failure-revision','delete-failure-set','delete-failure-epoch')`,
		`INSERT INTO akos_withdraw_v1.index_generations(space_id,kb_id,document_id,revision_id,generation_set_id,id,backend_epoch_id,channel,profile_digest,module_instance_id) VALUES('space','kb','delete-failure-document','delete-failure-revision','delete-failure-set','delete-failure-generation','delete-failure-epoch','keyword',repeat('a',64),'delete-failure-engine')`,
		`INSERT INTO akos_withdraw_v1.binding_definitions VALUES('space','kb','delete-failure-binding','delete-failure-engine',repeat('a',64),'index','{"generation_id":"delete-failure-generation"}')`,
		`INSERT INTO akos_withdraw_v1.index_projection_rows VALUES('delete-failure-generation','delete-failure-chunk','{"text":"retained fault fixture"}')`,
		`UPDATE akos_withdraw_v1.documents SET active_revision_id='delete-failure-revision',active_generation_set_id='delete-failure-set',row_version=2 WHERE id='delete-failure-document'`,
	}
	for _, statement := range statements {
		if _, e := db.ExecContext(ctx, statement); e != nil {
			return contracts.DeleteDocumentResult{}, e
		}
	}
	deadline, _ := ctx.Deadline()
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.Action != "document.delete" || s.SubjectID != "worker-a" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("failure source governance fixture denied")
		}
		return nil
	}
	authorize := func(c context.Context, q contracts.DeleteDocumentRequest) error {
		if q.DocumentID != "delete-failure-document" {
			return errors.New("failure source fixture resource denied")
		}
		return verify(c, q.Command.Scope)
	}
	request := contracts.DeleteDocumentRequest{DocumentID: "delete-failure-document", ExpectedDocumentVersion: 2, Command: contracts.CommandContext{Principal: contracts.Principal{SubjectID: "worker-a", SubjectType: "user", Issuer: "fixture"}, Scope: contracts.AccessScope{ScopeID: "failure-source-fixture", SubjectID: "worker-a", Action: "document.delete", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: deadline}, Request: contracts.RequestContext{RequestID: "failure-source-fixture", Deadline: deadline, ProfileDigest: profile}, IdempotencyKey: "failure-source-fixture"}}
	return (document.DeletionService{Store: postgres.DocumentDeletions{DB: db, Verify: verify, AuthorizeDelete: authorize}, Verify: authorize}).Delete(ctx, request)
}
