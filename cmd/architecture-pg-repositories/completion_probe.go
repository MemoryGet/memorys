package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

var completionSamples []preparation.CompletionResult
var completionJobSamples []map[string]any

type lostCompletionResponse struct{ inner preparation.CompletionStore }

func (p lostCompletionResponse) Complete(ctx context.Context, r preparation.CompletionRequest) (preparation.CompletionResult, error) {
	if _, err := p.inner.Complete(ctx, r); err != nil {
		return preparation.CompletionResult{}, err
	}
	return preparation.CompletionResult{}, document.ErrPublicationTaskUnknown
}

func verifyPublicationCompletion(ctx context.Context, db *sql.DB, tasks postgres.PublicationTasks, makeAccess func(string, time.Duration) (publicationTaskFixture, document.PublicationReadWorkRequest, error), worker preparation.Worker, backend postgres.PreparationIndex) error {
	store := postgres.PublicationCompletion{Tasks: tasks, ModuleInstanceID: backend.ModuleInstanceID}
	prepared := func(name string, duration time.Duration) (publicationTaskFixture, preparation.CompletionRequest, error) {
		f, access, err := makeAccess(name, duration)
		if err != nil {
			return f, preparation.CompletionRequest{}, err
		}
		a, err := worker.Prepare(ctx, access)
		if err != nil {
			return f, preparation.CompletionRequest{}, err
		}
		work, err := tasks.ReadOwned(ctx, access)
		if err != nil {
			return f, preparation.CompletionRequest{}, err
		}
		return f, preparation.CompletionRequest{Access: access, CommandID: contracts.ID(name + "-complete"), ArtifactDigest: a.Digest, ExpectedJobVersion: work.Job.RowVersion}, nil
	}
	load := func(id contracts.ID) (contracts.Job, error) {
		var raw []byte
		var job contracts.Job
		err := db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.publication_task_heads WHERE id=$1`, string(id)).Scan(&raw)
		if err != nil {
			return job, err
		}
		err = json.Unmarshal(raw, &job)
		return job, err
	}
	assertRunning := func(r preparation.CompletionRequest) error {
		job, err := load(r.Access.Lease.JobID)
		if err != nil || job.State != "running" || job.RowVersion != r.ExpectedJobVersion {
			return errors.New("completion rollback changed task")
		}
		for _, table := range []string{"publication_completions", "publication_completion_audit", "publication_completion_outbox"} {
			var n int
			if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.`+table+` WHERE job_id=$1`, string(job.ID)).Scan(&n) != nil || n != 0 {
				return errors.New("partial completion effects persisted")
			}
		}
		return nil
	}
	f, r, err := prepared("completion-durable", 700*time.Millisecond)
	if err != nil {
		return err
	}
	if result, err := (lostCompletionResponse{store}).Complete(ctx, r); !errors.Is(err, document.ErrPublicationTaskUnknown) || result.Job.ID != "" {
		return errors.New("completion response loss not unknown")
	}
	cold, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cold.Close()
	coldTasks := tasks
	coldTasks.DB = cold
	coldStore := store
	coldStore.Tasks = coldTasks
	// Historical completion remains recoverable after the execution lease expires.
	if _, err := db.ExecContext(ctx, `SELECT pg_sleep(0.75)`); err != nil {
		return err
	}
	r.Access.Request.RequestID = "completion-cold-replay"
	result, err := coldStore.Complete(ctx, r)
	if err != nil || result.Job.State != "completed" || result.Job.RowVersion != r.ExpectedJobVersion+1 || result.Job.LeaseUntil != nil || result.Job.Progress != 1 || result.Job.GenerationID == nil {
		return errors.New("cold completion replay differs")
	}

	result.Artifact.Profile.Routes[0].ID = "mutated-caller"
	again, err := coldStore.Complete(ctx, r)
	if err != nil || reflect.DeepEqual(again, result) || again.Artifact.Profile.Routes[0].ID != "keyword" {
		return errors.New("completion response aliased storage")
	}
	completionSamples = append(completionSamples, again)
	completionJobSamples = append(completionJobSamples, publicationTaskJobSample(again.Job))
	for _, table := range []string{"publication_completions", "publication_completion_audit", "publication_completion_outbox"} {
		var n int
		if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.`+table+` WHERE job_id=$1`, string(r.Access.Lease.JobID)).Scan(&n) != nil || n != 1 {
			return errors.New("completion replay duplicated effects")
		}
	}
	if _, err := tasks.ReadOwned(ctx, r.Access); err == nil {
		return errors.New("completed task released source")
	}
	if _, err := tasks.Renew(ctx, document.PublicationRenewRequest{Request: r.Access.Request, Scope: r.Access.Scope, Lease: r.Access.Lease, Duration: time.Second, CommandID: "renew-completed"}); err == nil {
		return errors.New("completed task renewed")
	}
	if _, err := tasks.Claim(ctx, document.PublicationClaimRequest{CommandID: "claim-completed", ClaimRequest: contracts.ClaimRequest{Request: r.Access.Request, Scope: r.Access.Scope, JobID: r.Access.Lease.JobID, WorkerID: r.Access.Lease.WorkerID, LeaseDuration: time.Second}}); err == nil {
		return errors.New("completed task reclaimed")
	}
	for _, change := range []func(*preparation.CompletionRequest){func(r *preparation.CompletionRequest) {
		r.ArtifactDigest = contracts.Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	}, func(r *preparation.CompletionRequest) { r.ExpectedJobVersion++ }, func(r *preparation.CompletionRequest) { r.Access.Lease.FencingToken++ }, func(r *preparation.CompletionRequest) { r.CommandID = "different-completion-command" }} {
		bad := r
		change(&bad)
		if v, err := store.Complete(ctx, bad); err == nil || v.Job.ID != "" {
			return errors.New("completion semantic conflict accepted")
		}
	}
	denied := tasks
	denied.AuthorizeTask = func(context.Context, document.PublicationTaskAccess) error { return errors.New("worker revoked") }
	if _, err := (postgres.PublicationCompletion{Tasks: denied, ModuleInstanceID: backend.ModuleInstanceID}).Complete(ctx, r); err == nil {
		return errors.New("historical completion bypassed worker authority")
	}
	denied = tasks
	denied.AuthorizePublisher = func(context.Context, document.PublicationStartPlan) error { return errors.New("publisher revoked") }
	if _, err := (postgres.PublicationCompletion{Tasks: denied, ModuleInstanceID: backend.ModuleInstanceID}).Complete(ctx, r); err == nil {
		return errors.New("historical completion bypassed publisher authority")
	}
	var inactive bool
	if db.QueryRowContext(ctx, `SELECT publication_epoch=0 AND active_revision_id IS NULL AND active_generation_set_id IS NULL FROM akos_withdraw_v1.documents WHERE id=$1`, string(f.Approved.Revision.DocumentID)).Scan(&inactive) != nil || !inactive {
		return errors.New("completion published content")
	}
	// Independent backend inspection must reject a prepared index deleted before completion.
	f, r, err = prepared("completion-deleted-index", 4*time.Second)
	if err != nil {
		return err
	}
	work, err := tasks.ReadOwned(ctx, r.Access)
	if err != nil {
		return err
	}
	_, generation := preparation.TargetIDs(work.Job.OperationID, r.Access.Lease.AttemptID)
	deleteScope := r.Access.Scope
	deleteScope.Action = "index.delete"
	cleanup := postgres.IndexCleanup{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }}
	if err := cleanup.Delete(ctx, contracts.IndexDeleteRequest{Request: r.Access.Request, Scope: deleteScope, GenerationID: generation}); err != nil {
		return err
	}
	if _, err := store.Complete(ctx, r); err == nil {
		return errors.New("deleted preparation completed")
	}
	if err := assertRunning(r); err != nil {
		return err
	}
	// Same attempt with a stale exact lease/version is rejected after renewal.
	_, r, err = prepared("completion-renewed", 500*time.Millisecond)
	if err != nil {
		return err
	}
	renewed, err := tasks.Renew(ctx, document.PublicationRenewRequest{Request: r.Access.Request, Scope: r.Access.Scope, Lease: r.Access.Lease, Duration: 4 * time.Second, CommandID: "completion-renew-lease"})
	if err != nil {
		return err
	}
	if _, err := store.Complete(ctx, r); err == nil {
		return errors.New("old completion lease survived renewal")
	}
	r.Access.Lease = renewed.Lease
	r.ExpectedJobVersion = renewed.JobVersion
	current, err := store.Complete(ctx, r)
	if err != nil {
		return errors.New("renewed exact lease could not complete")
	}
	completionSamples = append(completionSamples, current)
	completionJobSamples = append(completionJobSamples, publicationTaskJobSample(current.Job))
	// Actual expiry and takeover fence the old prepared artifact.
	_, r, err = prepared("completion-takeover", 300*time.Millisecond)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `SELECT pg_sleep(0.35)`); err != nil {
		return err
	}
	s := r.Access.Scope
	s.SubjectID = "worker-b"
	if _, err := tasks.Claim(ctx, document.PublicationClaimRequest{CommandID: "completion-takeover", ClaimRequest: contracts.ClaimRequest{Request: r.Access.Request, Scope: s, JobID: r.Access.Lease.JobID, WorkerID: "worker-b", LeaseDuration: time.Second}}); err != nil {
		return err
	}
	if _, err := store.Complete(ctx, r); err == nil {
		return errors.New("old attempt completed after takeover")
	}
	for _, table := range []string{"publication_completion_audit", "publication_completion_outbox"} {
		_, r, err = prepared("completion-fail-"+table, 4*time.Second)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.fail_completion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture completion failure'; END $$`); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `CREATE TRIGGER completion_failure BEFORE INSERT ON akos_withdraw_v1.`+table+` FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fail_completion()`); err != nil {
			return err
		}
		_, callErr := store.Complete(ctx, r)
		_, dropErr := db.ExecContext(ctx, `DROP TRIGGER completion_failure ON akos_withdraw_v1.`+table)
		if dropErr != nil {
			return dropErr
		}
		if callErr == nil {
			return errors.New("partial completion completed")
		}
		if err := assertRunning(r); err != nil {
			return err
		}
	}
	_, r, err = prepared("completion-missing-event", 4*time.Second)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.skip_completion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER completion_skip BEFORE INSERT ON akos_withdraw_v1.publication_completion_outbox FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.skip_completion()`); err != nil {
		return err
	}
	_, callErr := store.Complete(ctx, r)
	_, dropErr := db.ExecContext(ctx, `DROP TRIGGER completion_skip ON akos_withdraw_v1.publication_completion_outbox`)
	if dropErr != nil {
		return dropErr
	}
	if !errors.Is(callErr, document.ErrPublicationTaskUnknown) {
		return errors.New("missing event escaped commit gate")
	}
	if err := assertRunning(r); err != nil {
		return err
	}
	if _, err := store.Complete(ctx, r); err != nil {
		return err
	}
	// Commit-time authorization rollback, while generation and task locks are held.
	_, r, err = prepared("completion-revoked", 4*time.Second)
	if err != nil {
		return err
	}
	revoking := tasks
	calls := 0
	revoking.AuthorizeTask = func(c context.Context, a document.PublicationTaskAccess) error {
		if a.Kind == "complete" {
			calls++
			if calls == 3 {
				return errors.New("completion authority revoked")
			}
		}
		return tasks.AuthorizeTask(c, a)
	}
	if _, err := (postgres.PublicationCompletion{Tasks: revoking, ModuleInstanceID: backend.ModuleInstanceID}).Complete(ctx, r); err == nil || calls != 3 {
		return errors.New("precommit completion revocation ignored")
	}
	if err := assertRunning(r); err != nil {
		return err
	}
	// No raw success transition can bypass the completion receipt.
	_, r, err = prepared("completion-expired-at-commit", 350*time.Millisecond)
	if err != nil {
		return err
	}
	expiring := tasks
	expiryCalls := 0
	expiring.AuthorizeTask = func(c context.Context, a document.PublicationTaskAccess) error {
		if a.Kind == "complete" {
			expiryCalls++
			if expiryCalls == 3 {
				if _, err := db.ExecContext(c, `SELECT pg_sleep(0.45)`); err != nil {
					return err
				}
			}
		}
		return tasks.AuthorizeTask(c, a)
	}
	if _, err := (postgres.PublicationCompletion{Tasks: expiring, ModuleInstanceID: backend.ModuleInstanceID}).Complete(ctx, r); err == nil || expiryCalls != 3 {
		return errors.New("completion committed after lease expired")
	}
	if err := assertRunning(r); err != nil {
		return err
	}
	// A configuration ABA/version change supersedes an uncompleted plan.
	_, r, err = prepared("completion-config-stale", 4*time.Second)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.publication_backend_heads SET row_version=row_version+1 WHERE space_id='space' AND kb_id='kb'`); err != nil {
		return err
	}
	if _, err := store.Complete(ctx, r); !errors.Is(err, document.ErrPublicationSuperseded) {
		return errors.New("configuration-stale plan completed")
	}
	if err := assertRunning(r); err != nil {
		return err
	}
	_, r, err = prepared("completion-head-only", 4*time.Second)
	if err != nil {
		return err
	}
	job, err := load(r.Access.Lease.JobID)
	if err != nil {
		return err
	}
	job.State = "completed"
	job.RowVersion++
	job.Progress = 1
	job.LeaseUntil = nil
	_, generation = preparation.TargetIDs(job.OperationID, r.Access.Lease.AttemptID)
	job.GenerationID = &generation
	raw, _ := json.Marshal(job)
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.publication_task_heads SET state='completed',row_version=row_version+1,worker_id=NULL,attempt_id=NULL,lease_until=NULL,payload=$2::jsonb WHERE id=$1`, string(job.ID), string(raw)); err == nil {
		return errors.New("head-only success committed")
	}
	if err := assertRunning(r); err != nil {
		return err
	}
	// Two same-command finishers converge on one immutable result.
	_, r, err = prepared("completion-concurrent", 4*time.Second)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	wins := make(chan bool, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := store.Complete(ctx, r); wins <- e == nil }()
	}
	wg.Wait()
	close(wins)
	n := 0
	for win := range wins {
		if win {
			n++
		}
	}
	if n < 1 {
		return errors.New("concurrent completion had no winner")
	}
	if _, err := coldStore.Complete(ctx, r); err != nil {
		return errors.New("concurrent completion could not reconcile")
	}
	return verifyPublicationFinalization(ctx, db, tasks, prepared, worker, backend)
}
