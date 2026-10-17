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
	"github.com/MemoryGet/memorys/pkg/withdrawal"
)

var finalizationResults []preparation.PublicationResult
var finalizationRequests []preparation.PublicationRequest
var finalizationContractSamples []map[string]any

type lostFinalizationResponse struct{ inner preparation.PublicationStore }

func (p lostFinalizationResponse) Finalize(ctx context.Context, r preparation.PublicationRequest) (preparation.PublicationResult, error) {
	if _, err := p.inner.Finalize(ctx, r); err != nil {
		return preparation.PublicationResult{}, err
	}
	return preparation.PublicationResult{}, preparation.ErrPublicationUnknown
}

func recordFinalization(r preparation.PublicationRequest, v preparation.PublicationResult) {
	finalizationRequests = append(finalizationRequests, r)
	finalizationResults = append(finalizationResults, v)
	o, h := v.Operation, v.History
	finalizationContractSamples = append(finalizationContractSamples, map[string]any{"schema": "PublicationOperation", "payload": map[string]any{"operation_id": o.ID, "document_id": o.DocumentID, "revision_id": o.RevisionID, "approval_id": o.ApprovalID, "expected_epoch": o.ExpectedEpoch, "state": o.State, "generation_set_id": o.GenerationSetID}}, map[string]any{"schema": "PublicationHistory", "payload": map[string]any{"id": h.ID, "document_id": h.DocumentID, "publication_epoch": h.PublicationEpoch, "revision_id": h.RevisionID, "action": h.Action, "actor_id": h.ActorID, "reason": h.Reason, "occurred_at": h.OccurredAt}})
}

func verifyPublicationFinalization(ctx context.Context, db *sql.DB, tasks postgres.PublicationTasks, prepared func(string, time.Duration) (publicationTaskFixture, preparation.CompletionRequest, error), worker preparation.Worker, backend postgres.PreparationIndex) error {
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || s.SubjectID != "publisher" || s.Action != "publication.publish" {
			return errors.New("final publisher fixture denied")
		}
		return nil
	}
	authorize := func(c context.Context, r preparation.PublicationRequest, p document.PublicationStartPlan) error {
		if verify(c, r.Command.Scope) != nil || r.Command.Principal.Issuer != "fixture" || r.OperationID != p.Result.Operation.ID {
			return errors.New("final resource authority fixture denied")
		}
		return nil
	}
	store := postgres.PublicationFinalizer{DB: db, Verify: verify, AuthorizePublication: authorize, ModuleInstanceID: backend.ModuleInstanceID}
	complete := postgres.PublicationCompletion{Tasks: tasks, ModuleInstanceID: backend.ModuleInstanceID}
	requestFor := func(c preparation.CompletionRequest, v preparation.CompletionResult, name string, expected uint64) (preparation.PublicationRequest, error) {
		var payload []byte
		var plan document.PublicationStartPlan
		if db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.publication_intents WHERE id=$1`, string(v.Artifact.OperationID)).Scan(&payload) != nil || json.Unmarshal(payload, &plan) != nil {
			return preparation.PublicationRequest{}, errors.New("final fixture plan missing")
		}
		s := c.Access.Scope
		s.SubjectID = "publisher"
		s.Action = "publication.publish"
		s.ScopeID = "final-publication-scope"
		r := c.Access.Request
		r.RequestID = contracts.ID(name + "-finalize")
		r.ProfileDigest = plan.ProfileDigest
		return preparation.PublicationRequest{Command: contracts.CommandContext{Request: r, Scope: s, Principal: contracts.Principal{SubjectID: "publisher", SubjectType: "user", Issuer: "fixture"}, IdempotencyKey: name}, OperationID: v.Artifact.OperationID, ExpectedEpoch: expected, ArtifactDigest: v.Artifact.Digest}, nil
	}
	ready := func(name string) (publicationTaskFixture, preparation.PublicationRequest, preparation.CompletionResult, error) {
		f, c, err := prepared(name, 4*time.Second)
		if err != nil {
			return f, preparation.PublicationRequest{}, preparation.CompletionResult{}, err
		}
		v, err := complete.Complete(ctx, c)
		if err != nil {
			return f, preparation.PublicationRequest{}, v, err
		}
		r, err := requestFor(c, v, name, 0)
		return f, r, v, err
	}
	assertInactive := func(f publicationTaskFixture) error {
		var inactive bool
		if db.QueryRowContext(ctx, `SELECT publication_epoch=0 AND active_revision_id IS NULL AND active_generation_set_id IS NULL FROM akos_withdraw_v1.documents WHERE id=$1`, string(f.Approved.Revision.DocumentID)).Scan(&inactive) != nil || !inactive {
			return errors.New("failed finalization changed publication")
		}
		for _, table := range []string{"publication_finalizations", "publication_publish_audit", "publication_publish_outbox", "publication_history", "command_operations"} {
			var n int
			if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.`+table+` WHERE `+map[bool]string{true: "operation_id", false: "id"}[table != "command_operations"]+`=$1`, string(f.Start.Operation.ID)).Scan(&n) != nil || n != 0 {
				return errors.New("finalization rollback left effects")
			}
		}
		return nil
	}
	f, r, c, err := ready("final-publication-first")
	if err != nil {
		return err
	}
	if v, err := (lostFinalizationResponse{store}).Finalize(ctx, r); !errors.Is(err, preparation.ErrPublicationUnknown) || v.Operation.ID != "" {
		return errors.New("final publication response loss not unknown")
	}
	cold, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cold.Close()
	coldStore := store
	coldStore.DB = cold
	r.Command.Request.RequestID = "cold-finalization-replay"
	first, err := coldStore.Finalize(ctx, r)
	if err != nil || first.History.PublicationEpoch != 1 || first.Operation.State != "committed" || first.CleanupPlanID != nil {
		return errors.New("finalization cold replay failed")
	}
	recordFinalization(r, first)
	for _, table := range []string{"publication_finalizations", "publication_publish_audit", "publication_publish_outbox"} {
		var n int
		if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.`+table+` WHERE operation_id=$1`, string(r.OperationID)).Scan(&n) != nil || n != 1 {
			return errors.New("finalization replay duplicated effects")
		}
	}
	for _, mutate := range []func(*preparation.PublicationRequest){func(r *preparation.PublicationRequest) { r.ExpectedEpoch++ }, func(r *preparation.PublicationRequest) {
		r.ArtifactDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}, func(r *preparation.PublicationRequest) { r.Command.IdempotencyKey = "different-finalization-key" }, func(r *preparation.PublicationRequest) { r.Command.Principal.Issuer = "another-issuer" }} {
		bad := r
		mutate(&bad)
		if _, err := store.Finalize(ctx, bad); err == nil {
			return errors.New("conflicting finalization replay accepted")
		}
	}
	denied := store
	denied.AuthorizePublication = func(context.Context, preparation.PublicationRequest, document.PublicationStartPlan) error {
		return errors.New("publisher revoked")
	}
	if _, err := denied.Finalize(ctx, r); err == nil {
		return errors.New("finalization replay bypassed current publisher authority")
	}
	// Prepare two replacements of the same approved Revision. Platform citations
	// stay stable; actual generation sets differ and only one expected-epoch CAS wins.
	registered := map[contracts.ID]bool{}
	replacementTasks := tasks
	replacementTasks.AuthorizeTask = func(cx context.Context, a document.PublicationTaskAccess) error {
		if registered[a.JobID] {
			return tasks.Verify(cx, a.Scope)
		}
		return tasks.AuthorizeTask(cx, a)
	}
	replacementBackend := backend
	replacementBackend.Tasks = replacementTasks
	replacementWorker := worker
	replacementWorker.Tasks = replacementTasks
	replacementWorker.Backend = replacementBackend
	makeReplacement := func(name string) (preparation.PublicationRequest, preparation.CompletionResult, error) {
		command := r.Command
		command.Scope.Action = "publication.start"
		command.IdempotencyKey = "start-" + name
		command.Request.RequestID = contracts.ID(name + "-start")
		starts := postgres.PublicationStarts{DB: db, ProfileDigest: command.Request.ProfileDigest, Verify: func(cx context.Context, s contracts.AccessScope) error {
			if cx.Err() != nil || s.Action != "publication.start" || s.SubjectID != "publisher" || s.SpaceID != "space" {
				return errors.New("replacement start scope denied")
			}
			return nil
		}, AuthorizeStart: func(context.Context, contracts.PublishRequest) error { return nil }}
		started, err := starts.StartPublication(ctx, contracts.PublishRequest{Command: command, DocumentID: f.Approved.Revision.DocumentID, RevisionID: f.Approved.Revision.ID, ApprovalID: f.Approved.Approval.ID, ExpectedEpoch: 1})
		if err != nil {
			return preparation.PublicationRequest{}, preparation.CompletionResult{}, err
		}
		registered[started.Job.ID] = true
		s := command.Scope
		s.SubjectID = "worker-a"
		s.Action = "publication.prepare"
		cr := command.Request
		cr.ProfileDigest = started.Job.ProfileDigest
		lease, err := replacementTasks.Claim(ctx, document.PublicationClaimRequest{CommandID: contracts.ID(name + "-claim"), ClaimRequest: contracts.ClaimRequest{Request: cr, Scope: s, JobID: started.Job.ID, WorkerID: "worker-a", LeaseDuration: 4 * time.Second}})
		if err != nil {
			return preparation.PublicationRequest{}, preparation.CompletionResult{}, err
		}
		access := document.PublicationReadWorkRequest{Request: cr, Scope: s, Lease: lease.Lease}
		a, err := replacementWorker.Prepare(ctx, access)
		if err != nil {
			return preparation.PublicationRequest{}, preparation.CompletionResult{}, err
		}
		completion := preparation.CompletionRequest{Access: access, CommandID: contracts.ID(name + "-complete"), ArtifactDigest: a.Digest, ExpectedJobVersion: lease.JobVersion}
		v, err := (postgres.PublicationCompletion{Tasks: replacementTasks, ModuleInstanceID: backend.ModuleInstanceID}).Complete(ctx, completion)
		if err != nil {
			return preparation.PublicationRequest{}, v, err
		}
		next, err := requestFor(completion, v, name, 1)
		return next, v, err
	}
	a, ac, err := makeReplacement("replacement-a")
	if err != nil {
		return err
	}
	b, bc, err := makeReplacement("replacement-b")
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.fail_finalization() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture finalization failure'; END $$`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER finalization_failure BEFORE INSERT ON akos_withdraw_v1.publication_publish_outbox FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fail_finalization()`); err != nil {
		return err
	}
	_, failed := store.Finalize(ctx, a)
	_, drop := db.ExecContext(ctx, `DROP TRIGGER finalization_failure ON akos_withdraw_v1.publication_publish_outbox`)
	if drop != nil {
		return drop
	}
	if failed == nil {
		return errors.New("replacement with failed event committed")
	}
	var preserved bool
	if db.QueryRowContext(ctx, `SELECT publication_epoch=1 AND active_generation_set_id=$2 FROM akos_withdraw_v1.documents WHERE id=$1`, string(f.Approved.Revision.DocumentID), string(c.Artifact.GenerationSet.ID)).Scan(&preserved) != nil || !preserved {
		return errors.New("failed replacement damaged old publication")
	}
	type outcome struct {
		request preparation.PublicationRequest
		result  preparation.PublicationResult
		err     error
	}
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, candidate := range []preparation.PublicationRequest{a, b} {
		wg.Add(1)
		go func(q preparation.PublicationRequest) {
			defer wg.Done()
			v, e := store.Finalize(ctx, q)
			outcomes <- outcome{q, v, e}
		}(candidate)
	}
	wg.Wait()
	close(outcomes)
	var winner outcome
	wins := 0
	for o := range outcomes {
		if o.err == nil {
			winner = o
			wins++
		}
	}
	if wins != 1 || winner.result.CleanupPlanID == nil || winner.result.History.PublicationEpoch != 2 {
		return errors.New("replacement expected-epoch CAS did not select one winner")
	}
	second, err := coldStore.Finalize(ctx, winner.request)
	if err != nil || !reflect.DeepEqual(second, winner.result) {
		return errors.New("replacement replay differs")
	}
	recordFinalization(winner.request, second)
	winnerCompletion := ac
	if winner.request.OperationID == b.OperationID {
		winnerCompletion = bc
	}
	// Existing cleanup storage can claim the fixed old index target. Its actual
	// deletion must leave the new set's records and owner untouched.
	s := r.Command.Scope
	s.SubjectID = "worker-a"
	s.Action = "publication.cleanup.execute"
	cleanupTasks := postgres.CleanupTasks{DB: db, Verify: func(cx context.Context, s contracts.AccessScope) error {
		if cx.Err() != nil || s.Action != "publication.cleanup.execute" || s.SubjectID != "worker-a" {
			return errors.New("cleanup scope denied")
		}
		return nil
	}}
	var target string
	if db.QueryRowContext(ctx, `SELECT id FROM akos_withdraw_v1.cleanup_targets WHERE plan_id=$1`, string(*second.CleanupPlanID)).Scan(&target) != nil {
		return errors.New("replacement lost old cleanup target")
	}
	lease, err := cleanupTasks.Claim(ctx, s, *second.CleanupPlanID, contracts.ID(target), "worker-a", time.Second)
	if err != nil || lease.GenerationSetID != c.Artifact.GenerationSet.ID || lease.PublicationEpoch != 1 {
		return errors.New("replacement cleanup lease targets wrong source")
	}
	s.Action = "index.delete"
	cleanup := postgres.IndexCleanup{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }}
	if err := cleanup.Delete(ctx, contracts.IndexDeleteRequest{Request: r.Command.Request, Scope: s, GenerationID: *lease.Target.GenerationID}); err != nil {
		return err
	}
	state, err := cleanup.InspectGeneration(ctx, s, winnerCompletion.Artifact.GenerationSet.Channels[contracts.KeywordChannel].ID)
	if err != nil || state.Deleted || state.RecordCount != 2 {
		return errors.New("old cleanup damaged new publication")
	}
	// The normal withdrawal service accepts the new authoritative publication.
	withdrawCommand := winner.request.Command
	withdrawCommand.Scope.Action = "publication.withdraw"
	withdrawCommand.IdempotencyKey = "withdraw-finalized-source"
	preparer := withdrawal.Preparer{Verify: func(context.Context, contracts.WithdrawRequest) error { return nil }}
	service := withdrawal.Service{Transactions: postgres.WithdrawalAdapter{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }}, Prepare: preparer.Prepare}
	withdrawn, err := service.Withdraw(ctx, contracts.WithdrawRequest{Command: withdrawCommand, DocumentID: f.Approved.Revision.DocumentID, ExpectedEpoch: 2, Reason: "实际发布后的撤回"})
	if err != nil || withdrawn.PublicationEpoch != 3 {
		return errors.New("new publication cannot use existing withdrawal transaction")
	}
	if replayed, err := coldStore.Finalize(ctx, winner.request); err != nil || !reflect.DeepEqual(replayed, second) {
		return errors.New("historical publication could not replay after withdrawal")
	}
	if db.QueryRowContext(ctx, `SELECT publication_epoch=3 AND active_revision_id IS NULL AND active_generation_set_id IS NULL FROM akos_withdraw_v1.documents WHERE id=$1`, string(f.Approved.Revision.DocumentID)).Scan(&preserved) != nil || !preserved {
		return errors.New("publication replay resurrected withdrawn source")
	}
	// Deleted actual index and a revoked approval invalidate already-completed tasks.
	other, q, completed, err := ready("finalization-deleted")
	if err != nil {
		return err
	}
	s = q.Command.Scope
	s.Action = "index.delete"
	if err := cleanup.Delete(ctx, contracts.IndexDeleteRequest{Request: q.Command.Request, Scope: s, GenerationID: completed.Artifact.GenerationSet.Channels[contracts.KeywordChannel].ID}); err != nil {
		return err
	}
	if _, err := store.Finalize(ctx, q); err == nil {
		return errors.New("deleted completed index published")
	}
	if err := assertInactive(other); err != nil {
		return err
	}
	other, q, _, err = ready("finalization-revoked-review")
	if err != nil {
		return err
	}
	reviewCommand := q.Command
	reviewCommand.Principal = contracts.Principal{SubjectID: "reviewer", SubjectType: "user", Issuer: "fixture"}
	reviewCommand.Scope.SubjectID = "reviewer"
	reviewCommand.Scope.Action = "revision.approve"
	reviewCommand.IdempotencyKey = "revoke-completed-before-publish"
	reviews := postgres.Reviews{DB: db, Verify: func(context.Context, contracts.AccessScope) error { return nil }, AuthorizeDecision: func(context.Context, document.ReviewCommand) error { return nil }, ProfileDigest: reviewCommand.Request.ProfileDigest, ReviewPolicyVersion: 7}
	if _, err := reviews.DecideReview(ctx, document.ReviewCommand{DocumentID: other.Approved.Revision.DocumentID, Request: contracts.ApprovalDecisionRequest{Command: reviewCommand, RevisionID: other.Approved.Revision.ID, ExpectedApprovalVersion: other.Approved.Approval.RowVersion, Decision: "revoke", Reason: "发布前撤销"}}); err != nil {
		return err
	}
	if _, err := store.Finalize(ctx, q); err == nil {
		return errors.New("revoked completed source published")
	}
	if err := assertInactive(other); err != nil {
		return err
	}
	for _, table := range []string{"publication_publish_audit", "publication_publish_outbox"} {
		other, q, _, err = ready("final-fail-" + table)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `CREATE TRIGGER finalization_failure BEFORE INSERT ON akos_withdraw_v1.`+table+` FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fail_finalization()`); err != nil {
			return err
		}
		_, callErr := store.Finalize(ctx, q)
		_, dropErr := db.ExecContext(ctx, `DROP TRIGGER finalization_failure ON akos_withdraw_v1.`+table)
		if dropErr != nil {
			return dropErr
		}
		if callErr == nil {
			return errors.New("partial finalization committed")
		}
		if err := assertInactive(other); err != nil {
			return err
		}
	}
	other, q, _, err = ready("final-missing-event")
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.skip_finalization() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER finalization_skip BEFORE INSERT ON akos_withdraw_v1.publication_publish_outbox FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.skip_finalization()`); err != nil {
		return err
	}
	_, callErr := store.Finalize(ctx, q)
	_, dropErr := db.ExecContext(ctx, `DROP TRIGGER finalization_skip ON akos_withdraw_v1.publication_publish_outbox`)
	if dropErr != nil {
		return dropErr
	}
	if !errors.Is(callErr, preparation.ErrPublicationUnknown) {
		return errors.New("missing final event escaped commit gate")
	}
	if err := assertInactive(other); err != nil {
		return err
	}
	if _, err := store.Finalize(ctx, q); err != nil {
		return err
	}
	other, q, _, err = ready("final-precommit-revocation")
	if err != nil {
		return err
	}
	revoking := store
	calls := 0
	revoking.AuthorizePublication = func(cx context.Context, r preparation.PublicationRequest, p document.PublicationStartPlan) error {
		calls++
		if calls == 3 {
			return errors.New("publisher revoked at final commit")
		}
		return authorize(cx, r, p)
	}
	if _, err := revoking.Finalize(ctx, q); err == nil || calls != 3 {
		return errors.New("finalization ignored precommit revocation")
	}
	if err := assertInactive(other); err != nil {
		return err
	}
	other, q, completed, err = ready("final-head-only")
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.documents SET publication_epoch=publication_epoch+1,row_version=row_version+1,active_revision_id=$2,active_generation_set_id=$3 WHERE id=$1`, string(other.Approved.Revision.DocumentID), string(other.Approved.Revision.ID), string(completed.Artifact.GenerationSet.ID)); err == nil {
		return errors.New("head-only prepared publication committed")
	}
	if err := assertInactive(other); err != nil {
		return err
	}
	other, q, completed, err = ready("final-readiness-lock")
	if err != nil {
		return err
	}
	locked := store
	lockCalls := 0
	locked.AuthorizePublication = func(cx context.Context, r preparation.PublicationRequest, p document.PublicationStartPlan) error {
		lockCalls++
		if lockCalls == 3 {
			for _, check := range []struct {
				query string
				id    contracts.ID
			}{{`SELECT id FROM akos_withdraw_v1.documents WHERE id=$1 FOR UPDATE NOWAIT`, other.Approved.Revision.DocumentID}, {`SELECT id FROM akos_withdraw_v1.index_generations WHERE id=$1 FOR UPDATE NOWAIT`, completed.Artifact.GenerationSet.Channels[contracts.KeywordChannel].ID}} {
				tx, err := cold.BeginTx(cx, nil)
				if err != nil {
					return err
				}
				var id string
				err = tx.QueryRowContext(cx, check.query, string(check.id)).Scan(&id)
				tx.Rollback()
				var code interface{ SQLState() string }
				if !errors.As(err, &code) || code.SQLState() != "55P03" {
					return errors.New("final publication did not hold source/index lock")
				}
			}
		}
		return authorize(cx, r, p)
	}
	if _, err := locked.Finalize(ctx, q); err != nil || lockCalls != 3 {
		return errors.New("final publication readiness write barrier failed")
	}
	other, q, _, err = ready("final-config-stale")
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.publication_backend_heads SET row_version=row_version+1 WHERE space_id='space' AND kb_id='kb'`); err != nil {
		return err
	}
	if _, err := store.Finalize(ctx, q); !errors.Is(err, document.ErrPublicationSuperseded) {
		return errors.New("configuration-stale completion published")
	}
	if err := assertInactive(other); err != nil {
		return err
	}
	originalFixture, originalRequest, _, err := ready("evidence-original")
	if err != nil {
		return err
	}
	if _, err := store.Finalize(ctx, originalRequest); err != nil {
		return err
	}
	_, deletionRequest, _, err := ready("keyword-deleted")
	if err != nil {
		return err
	}
	if _, err := store.Finalize(ctx, deletionRequest); err != nil {
		return err
	}
	historyFixture, historyRequest, historyCompletion, err := ready("history-source")
	if err != nil {
		return err
	}
	historyPublished, err := store.Finalize(ctx, historyRequest)
	if err != nil {
		return err
	}
	if err := createHistoricalReplacement(ctx, db, historyFixture, historyPublished, historyRequest.Command, tasks, worker, backend, store); err != nil {
		return err
	}
	historicalTarget.OldGeneration = historyCompletion.Artifact.GenerationSet.Channels[contracts.KeywordChannel].ID
	return verifyQueryReadPhase(db, r.Command, originalFixture.Approved.Originals)
}
