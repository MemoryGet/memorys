package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var publicationTaskSamples []map[string]any
var publicationLeaseSamples []document.PublicationLeaseResult

type publicationTaskFixture struct {
	Start    document.PublicationStartResult
	Approved approvedPublicationFixture
}

type lostPublicationTaskResponse struct{ inner document.PublicationTaskStore }

func (p lostPublicationTaskResponse) Claim(ctx context.Context, r document.PublicationClaimRequest) (document.PublicationLeaseResult, error) {
	if _, err := p.inner.Claim(ctx, r); err != nil {
		return document.PublicationLeaseResult{}, err
	}
	return document.PublicationLeaseResult{}, document.ErrPublicationTaskUnknown
}
func (p lostPublicationTaskResponse) Renew(ctx context.Context, r document.PublicationRenewRequest) (document.PublicationLeaseResult, error) {
	if _, err := p.inner.Renew(ctx, r); err != nil {
		return document.PublicationLeaseResult{}, err
	}
	return document.PublicationLeaseResult{}, document.ErrPublicationTaskUnknown
}

func verifyPublicationTasks(ctx context.Context, db *sql.DB, scope contracts.AccessScope, request contracts.RequestContext, newTask func(string) (publicationTaskFixture, error), revoke func(approvedPublicationFixture) error) error {
	jobs := map[contracts.ID]bool{}
	blocked := map[contracts.ID]bool{}
	create := func(name string) (publicationTaskFixture, error) {
		f, err := newTask(name)
		if err == nil {
			jobs[f.Start.Job.ID] = true
		}
		return f, err
	}
	scope.Action = "publication.prepare"
	workerScope := func(worker contracts.ID) contracts.AccessScope {
		s := scope
		s.SubjectID = worker
		s.ScopeID = "task-scope-" + worker
		return s
	}
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.Action != "publication.prepare" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || s.SubjectID != "worker-a" && s.SubjectID != "worker-b" {
			return errors.New("task worker fixture denied")
		}
		return nil
	}
	authorize := func(c context.Context, a document.PublicationTaskAccess) error {
		if verify(c, a.Scope) != nil || !jobs[a.JobID] || a.WorkerID != a.Scope.SubjectID {
			return errors.New("specific task fixture denied")
		}
		return nil
	}
	publisher := func(c context.Context, p document.PublicationStartPlan) error {
		if c.Err() != nil || p.OriginalPrincipal.SubjectID != "publisher" || p.OriginalPrincipal.Issuer != "fixture" || p.SpaceID != "space" || p.KnowledgeBaseID != "kb" || blocked[p.Result.Operation.ID] {
			return errors.New("original publisher fixture denied")
		}
		return nil
	}
	store := postgres.PublicationTasks{DB: db, Verify: verify, AuthorizeTask: authorize, AuthorizePublisher: publisher}
	claim := func(f publicationTaskFixture, worker, id contracts.ID, duration time.Duration) document.PublicationClaimRequest {
		r := request
		r.RequestID = id
		r.ProfileDigest = f.Start.Job.ProfileDigest
		return document.PublicationClaimRequest{CommandID: id, ClaimRequest: contracts.ClaimRequest{Request: r, Scope: workerScope(worker), JobID: f.Start.Job.ID, WorkerID: worker, LeaseDuration: duration}}
	}
	read := func(f publicationTaskFixture, lease contracts.Lease) document.PublicationReadWorkRequest {
		r := request
		r.RequestID = "read-owned-task"
		r.ProfileDigest = f.Start.Job.ProfileDigest
		return document.PublicationReadWorkRequest{Request: r, Scope: workerScope(lease.WorkerID), Lease: lease}
	}
	loadJob := func(id contracts.ID) (contracts.Job, error) {
		var payload []byte
		var job contracts.Job
		if err := db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.publication_task_heads WHERE id=$1`, string(id)).Scan(&payload); err != nil {
			return job, err
		}
		if json.Unmarshal(payload, &job) != nil {
			return job, errors.New("task job decode failed")
		}
		return job, nil
	}
	assertUnchanged := func(id contracts.ID) error {
		job, err := loadJob(id)
		if err != nil || job.RowVersion != 1 || job.State != "queued" || job.Attempt != 0 || job.FencingToken != 0 {
			return errors.New("rejected task mutated queued head")
		}
		var count int
		if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.publication_task_receipts WHERE job_id=$1`, string(id)).Scan(&count) != nil || count != 0 {
			return errors.New("rejected task retained receipt")
		}
		return nil
	}
	f, err := create("task-replay")
	if err != nil {
		return err
	}
	original := claim(f, "worker-a", "task-claim-original", time.Second)
	// The real claim commits, then its transport response is deliberately lost.
	if result, err := (lostPublicationTaskResponse{store}).Claim(ctx, original); !errors.Is(err, document.ErrPublicationTaskUnknown) || result.Lease.AttemptID != "" {
		return errors.New("real publication task claim response not lost")
	}
	var first document.PublicationLeaseResult
	var originalReceipt []byte
	if db.QueryRowContext(ctx, `SELECT result FROM akos_withdraw_v1.publication_task_receipts WHERE worker_id='worker-a' AND kind='claim' AND command_id=$1`, string(original.CommandID)).Scan(&originalReceipt) != nil || json.Unmarshal(originalReceipt, &first) != nil {
		return errors.New("actual committed claim receipt missing")
	}
	cold, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cold.Close()
	store.DB = cold
	claimRetry := original
	claimRetry.Request.RequestID = "task-claim-retry-trace"
	recovered, err := store.Claim(ctx, claimRetry)
	if err != nil || !reflect.DeepEqual(first, recovered) || first.JobVersion != 2 {
		return errors.New("cold claim replay changed attempt")
	}
	publicationLeaseSamples = append(publicationLeaseSamples, first)
	work, err := store.ReadOwned(ctx, read(f, first.Lease))
	if err != nil || work.Revision.ID != f.Approved.Revision.ID || work.Revision.ContentHash != f.Approved.Revision.ContentHash || work.Job.State != "running" || len(work.FrozenPayload) == 0 || work.Revision.Blocks[0].Content.Text != "人工修订" {
		return errors.New("owned task content binding failed")
	}
	publicationTaskSamples = append(publicationTaskSamples, publicationTaskJobSample(work.Job))
	work.FrozenPayload[0] = '!'
	work.Revision.Blocks[0].Content.Text = "caller edit"
	fresh, err := store.ReadOwned(ctx, read(f, first.Lease))
	if err != nil {
		return fmt.Errorf("task content reread rejected: lease_remaining=%s context=%v: %w", time.Until(first.Lease.ExpiresAt), ctx.Err(), err)
	}
	if len(fresh.FrozenPayload) == 0 || len(fresh.Revision.Blocks) == 0 || fresh.FrozenPayload[0] != '{' || fresh.Revision.Blocks[0].Content.Text != "人工修订" {
		return errors.New("task content aliased caller memory")
	}
	renewRequest := document.PublicationRenewRequest{Request: original.Request, Scope: original.Scope, Lease: first.Lease, Duration: 3 * time.Second, CommandID: "task-renew-original"}
	renewRequest.Request.RequestID = "task-renew-original"
	if result, err := (lostPublicationTaskResponse{store}).Renew(ctx, renewRequest); !errors.Is(err, document.ErrPublicationTaskUnknown) || result.Lease.AttemptID != "" {
		return errors.New("renewal response not lost")
	}
	var renewed document.PublicationLeaseResult
	if db.QueryRowContext(ctx, `SELECT result FROM akos_withdraw_v1.publication_task_receipts WHERE worker_id='worker-a' AND kind='renew' AND command_id=$1`, string(renewRequest.CommandID)).Scan(&originalReceipt) != nil || json.Unmarshal(originalReceipt, &renewed) != nil || renewed.JobVersion != 3 || renewed.Lease.FencingToken != first.Lease.FencingToken || renewed.Lease.AttemptID != first.Lease.AttemptID {
		return errors.New("task renewal changed attempt")
	}
	// Recover a lost renewal response after the old lease has really expired.
	if _, err := cold.ExecContext(ctx, `SELECT pg_sleep(1.05)`); err != nil {
		return err
	}
	renewRequest.Request.RequestID = "task-renew-retry-trace"
	recoveredRenew, err := store.Renew(ctx, renewRequest)
	if err != nil || !reflect.DeepEqual(recoveredRenew, renewed) {
		return errors.New("renewal replay after old expiry failed")
	}
	publicationLeaseSamples = append(publicationLeaseSamples, renewed)
	if body, err := store.ReadOwned(ctx, read(f, first.Lease)); err == nil || len(body.FrozenPayload) != 0 {
		return errors.New("old pre-renewal lease released content")
	}
	work, err = store.ReadOwned(ctx, read(f, renewed.Lease))
	if err != nil {
		return errors.New("renewed owner cannot load work")
	}
	publicationTaskSamples = append(publicationTaskSamples, publicationTaskJobSample(work.Job))
	for _, table := range []string{"publication_task_receipts", "publication_task_audit", "publication_task_outbox"} {
		var count int
		if cold.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.`+table+` WHERE job_id=$1`, string(f.Start.Job.ID)).Scan(&count) != nil || count != 2 {
			return errors.New("task replay duplicated audit or event")
		}
	}
	f, err = create("task-takeover")
	if err != nil {
		return err
	}
	aRequest := claim(f, "worker-a", "task-old-owner", 300*time.Millisecond)
	a, err := store.Claim(ctx, aRequest)
	if err != nil {
		return errors.New("takeover initial claim failed")
	}
	bRequest := claim(f, "worker-b", "task-next-owner", 2*time.Second)
	if result, err := store.Claim(ctx, bRequest); err == nil || result.Lease.AttemptID != "" {
		return errors.New("unexpired task displaced")
	}
	if _, err := cold.ExecContext(ctx, `SELECT pg_sleep(0.35)`); err != nil {
		return err
	}
	b, err := store.Claim(ctx, bRequest)
	if err != nil || b.Lease.FencingToken != a.Lease.FencingToken+1 || b.Lease.AttemptID == a.Lease.AttemptID || b.JobVersion != 3 {
		return errors.New("expired task takeover not fenced")
	}
	publicationLeaseSamples = append(publicationLeaseSamples, a, b)
	if body, err := store.ReadOwned(ctx, read(f, a.Lease)); err == nil || len(body.FrozenPayload) != 0 {
		return errors.New("old attempt read new work")
	}
	historical, err := store.Claim(ctx, aRequest)
	if err != nil || !reflect.DeepEqual(historical, a) {
		return errors.New("claim receipt changed after takeover")
	}
	if body, err := store.ReadOwned(ctx, read(f, historical.Lease)); err == nil || len(body.FrozenPayload) != 0 {
		return errors.New("old receipt recreated execution authority")
	}
	work, err = store.ReadOwned(ctx, read(f, b.Lease))
	if err != nil || work.Job.Attempt != 2 {
		return errors.New("new owner cannot load attempt")
	}
	publicationTaskSamples = append(publicationTaskSamples, publicationTaskJobSample(work.Job))
	blocked[f.Start.Operation.ID] = true
	if body, err := store.ReadOwned(ctx, read(f, b.Lease)); err == nil || len(body.FrozenPayload) != 0 {
		return errors.New("worker authority replaced original publisher")
	}
	blocked[f.Start.Operation.ID] = false
	denied := store
	denied.AuthorizeTask = nil
	if body, err := denied.ReadOwned(ctx, read(f, b.Lease)); err == nil || len(body.FrozenPayload) != 0 {
		return errors.New("missing task authorization allowed read")
	}
	for _, table := range []string{"publication_task_audit", "publication_task_outbox"} {
		f, err := create("fail-" + table)
		if err != nil {
			return err
		}
		r := claim(f, "worker-a", contracts.ID("fail-"+table), 2*time.Second)
		if _, err := cold.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.fail_publication_task() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture task failure'; END $$`); err != nil {
			return err
		}
		if _, err := cold.ExecContext(ctx, `CREATE TRIGGER task_failure BEFORE INSERT ON akos_withdraw_v1.`+table+` FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fail_publication_task()`); err != nil {
			return err
		}
		result, claimErr := store.Claim(ctx, r)
		_, dropErr := cold.ExecContext(ctx, `DROP TRIGGER task_failure ON akos_withdraw_v1.`+table)
		if dropErr != nil {
			return dropErr
		}
		if !errors.Is(claimErr, document.ErrPublicationTaskRejected) || result.Lease.AttemptID != "" {
			return errors.New("task write failure not rejected")
		}
		if err := assertUnchanged(f.Start.Job.ID); err != nil {
			return err
		}
	}
	f, err = create("missing-task-event")
	if err != nil {
		return err
	}
	missing := claim(f, "worker-a", "missing-task-event", 2*time.Second)
	if _, err := cold.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.skip_publication_task() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`); err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `CREATE TRIGGER task_skip BEFORE INSERT ON akos_withdraw_v1.publication_task_outbox FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.skip_publication_task()`); err != nil {
		return err
	}
	result, claimErr := store.Claim(ctx, missing)
	_, dropErr := cold.ExecContext(ctx, `DROP TRIGGER task_skip ON akos_withdraw_v1.publication_task_outbox`)
	if dropErr != nil {
		return dropErr
	}
	if !errors.Is(claimErr, document.ErrPublicationTaskUnknown) || result.Lease.AttemptID != "" {
		return errors.New("missing task event passed deferred gate")
	}
	if err := assertUnchanged(f.Start.Job.ID); err != nil {
		return err
	}
	if _, err := store.Claim(ctx, missing); err != nil {
		return errors.New("task could not retry confirmed rollback")
	}
	f, err = create("corrupt-task-receipt")
	if err != nil {
		return err
	}
	corrupt := claim(f, "worker-a", "corrupt-task-receipt", 2*time.Second)
	if _, err := cold.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.corrupt_task_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.result=jsonb_set(NEW.result,'{Lease,FencingToken}','999'); RETURN NEW; END $$`); err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `CREATE TRIGGER corrupt_task_receipt BEFORE INSERT ON akos_withdraw_v1.publication_task_receipts FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.corrupt_task_receipt()`); err != nil {
		return err
	}
	corruptResult, corruptErr := store.Claim(ctx, corrupt)
	_, dropErr = cold.ExecContext(ctx, `DROP TRIGGER corrupt_task_receipt ON akos_withdraw_v1.publication_task_receipts`)
	if dropErr != nil {
		return dropErr
	}
	if !errors.Is(corruptErr, document.ErrPublicationTaskUnknown) || corruptResult.Lease.AttemptID != "" {
		return errors.New("corrupt lease receipt passed commit gate")
	}
	if err := assertUnchanged(f.Start.Job.ID); err != nil {
		return err
	}
	f, err = create("revoke-task-authority")
	if err != nil {
		return err
	}
	revoking := store
	calls := 0
	revoking.AuthorizeTask = func(c context.Context, a document.PublicationTaskAccess) error {
		calls++
		if calls == 3 {
			return errors.New("task grant revoked at commit")
		}
		return authorize(c, a)
	}
	if result, err := revoking.Claim(ctx, claim(f, "worker-a", "revoke-task-authority", 2*time.Second)); err == nil || result.Lease.AttemptID != "" || calls != 3 {
		return errors.New("precommit task revocation accepted")
	}
	if err := assertUnchanged(f.Start.Job.ID); err != nil {
		return err
	}
	f, err = create("revoke-publisher-authority")
	if err != nil {
		return err
	}
	revoking = store
	publisherCalls := 0
	revoking.AuthorizePublisher = func(c context.Context, p document.PublicationStartPlan) error {
		publisherCalls++
		if publisherCalls == 2 {
			return errors.New("publisher revoked at commit")
		}
		return publisher(c, p)
	}
	if result, err := revoking.Claim(ctx, claim(f, "worker-a", "revoke-publisher-authority", 2*time.Second)); err == nil || result.Lease.AttemptID != "" || publisherCalls != 2 {
		return errors.New("precommit publisher revocation accepted")
	}
	if err := assertUnchanged(f.Start.Job.ID); err != nil {
		return err
	}
	f, err = create("race-task-claim")
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	wins := make(chan bool, 2)
	for _, worker := range []contracts.ID{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(worker contracts.ID) {
			defer wg.Done()
			_, err := store.Claim(ctx, claim(f, worker, "race-claim-"+worker, 2*time.Second))
			wins <- err == nil
		}(worker)
	}
	wg.Wait()
	close(wins)
	winners := 0
	for win := range wins {
		if win {
			winners++
		}
	}
	job, err := loadJob(f.Start.Job.ID)
	if err != nil || winners != 1 || job.Attempt != 1 || job.FencingToken != 1 || job.RowVersion != 2 {
		return errors.New("concurrent claim produced multiple owners")
	}
	assertSuperseded := func(f publicationTaskFixture, id contracts.ID) error {
		result, err := store.Claim(ctx, claim(f, "worker-a", id, 2*time.Second))
		if !errors.Is(err, document.ErrPublicationSuperseded) || result.Lease.AttemptID != "" {
			return errors.New("stale plan claimed")
		}
		job, err := loadJob(f.Start.Job.ID)
		if err != nil || job.State != "failed" || job.FencingToken != 1 || job.LeaseUntil != nil || job.Error == nil {
			return errors.New("superseded task not terminal or fenced")
		}
		publicationTaskSamples = append(publicationTaskSamples, publicationTaskJobSample(job))
		return nil
	}
	f, err = create("backend-stale-task")
	if err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `UPDATE akos_withdraw_v1.publication_backend_heads SET backend_epoch_id='publication-backend-1',row_version=row_version+1 WHERE space_id='space' AND kb_id='kb'`); err != nil {
		return err
	}
	if err := assertSuperseded(f, "backend-stale-task"); err != nil {
		return err
	}
	f, err = create("policy-aba-task")
	if err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `UPDATE akos_withdraw_v1.review_policy_heads SET policy_version=8,row_version=row_version+1 WHERE space_id='space' AND kb_id='kb'`); err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `UPDATE akos_withdraw_v1.review_policy_heads SET policy_version=9,row_version=row_version+1 WHERE space_id='space' AND kb_id='kb'`); err != nil {
		return err
	}
	if err := assertSuperseded(f, "policy-aba-task"); err != nil {
		return err
	}
	f, err = create("review-stale-task")
	if err != nil {
		return err
	}
	if err := revoke(f.Approved); err != nil {
		return err
	}
	if err := assertSuperseded(f, "review-stale-task"); err != nil {
		return err
	}
	f, err = create("epoch-stale-task")
	if err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `UPDATE akos_withdraw_v1.documents SET publication_epoch=publication_epoch+1,row_version=row_version+1 WHERE id=$1`, string(f.Approved.Revision.DocumentID)); err != nil {
		return err
	}
	if err := assertSuperseded(f, "epoch-stale-task"); err != nil {
		return err
	}
	var inactive bool
	if cold.QueryRowContext(ctx, `SELECT active_revision_id IS NULL AND active_generation_set_id IS NULL FROM akos_withdraw_v1.documents WHERE id=$1`, string(f.Approved.Revision.DocumentID)).Scan(&inactive) != nil || !inactive {
		return errors.New("task ownership published a revision")
	}
	return verifyPreparationIndex(ctx, db, scope, request, store, create)
}

func publicationTaskJobSample(job contracts.Job) map[string]any {
	payload := map[string]any{"id": job.ID, "space_id": job.SpaceID, "resource_id": job.OperationID, "state": job.State, "stage": job.Stage, "attempt": job.Attempt, "row_version": job.RowVersion, "progress": job.Progress}
	if job.Error != nil {
		payload["error"] = map[string]any{"code": job.Error.Code, "safe_message": job.Error.SafeMessage, "retryable": job.Error.Retryable, "request_id": job.Error.RequestID}
	}
	return map[string]any{"schema": "Job", "payload": payload}
}
