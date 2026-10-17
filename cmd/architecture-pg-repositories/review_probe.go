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

var reviewContractSamples []map[string]any

type lostReviewResponse struct{ repository document.ReviewRepository }

func (p lostReviewResponse) DecideReview(ctx context.Context, c document.ReviewCommand) (contracts.Approval, error) {
	if _, err := p.repository.DecideReview(ctx, c); err != nil {
		return contracts.Approval{}, err
	}
	return contracts.Approval{}, document.ErrReviewUnknown
}

func verifyReviewTransactions(ctx context.Context, db *sql.DB, scope contracts.AccessScope, request contracts.RequestContext, revision contracts.Revision, originals contracts.BlobStore, newRevision func(string) (contracts.SubmitResult, error)) error {
	scope.SubjectID = "reviewer"
	scope.Action = "revision.approve"
	scope.ScopeID = "review-scope"
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "reviewer" || s.SpaceID != "space" || s.Action != "revision.approve" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("review scope fixture denied")
		}
		return nil
	}
	authorize := func(c context.Context, cmd document.ReviewCommand) error {
		if verify(c, cmd.Request.Command.Scope) != nil || cmd.Request.Command.Principal.Issuer != "fixture" || cmd.Request.Command.Principal.SubjectType != "user" || cmd.DocumentID == "" || cmd.Request.RevisionID == "" {
			return errors.New("explicit human reviewer fixture denied")
		}
		return nil
	}
	repository := postgres.Reviews{DB: db, Verify: verify, AuthorizeDecision: authorize, ProfileDigest: request.ProfileDigest, ReviewPolicyVersion: 7}
	makeCommand := func(r contracts.Revision, key, decision string, expected uint64) document.ReviewCommand {
		return document.ReviewCommand{DocumentID: r.DocumentID, Request: contracts.ApprovalDecisionRequest{Command: contracts.CommandContext{Request: request, Scope: scope, Principal: contracts.Principal{SubjectID: "reviewer", SubjectType: "user", Issuer: "fixture"}, IdempotencyKey: key}, RevisionID: r.ID, Decision: decision, Reason: "人工逐项核对😀", ExpectedApprovalVersion: expected}}
	}
	load := func(r contracts.Revision) (contracts.ApprovalRequest, string, error) {
		var raw []byte
		var current sql.NullString
		var q contracts.ApprovalRequest
		if err := db.QueryRowContext(ctx, `SELECT payload,current_decision_id FROM akos_withdraw_v1.approval_requests WHERE revision_id=$1`, string(r.ID)).Scan(&raw, &current); err != nil {
			return q, "", err
		}
		if json.Unmarshal(raw, &q) != nil {
			return q, "", errors.New("review request decode")
		}
		return q, current.String, nil
	}
	initial, head, err := load(revision)
	if err != nil || initial.State != "submitted" || initial.RowVersion != 0 || head != "" {
		return errors.New("initial review contradicts management contract")
	}
	reviewContractSamples = append(reviewContractSamples, reviewRequestSample(initial))
	command := makeCommand(revision, "approve-original", "approve", 0)
	if result, err := (lostReviewResponse{repository}).DecideReview(ctx, command); !errors.Is(err, document.ErrReviewUnknown) || result.ID != "" {
		_, preparationErr := document.PrepareReview(initial, contracts.FrozenRevisionFact{ID: revision.ID, DocumentID: revision.DocumentID, HashFormat: revision.HashFormat, ContentHash: revision.ContentHash, BlocksHash: revision.BlocksHash}, 0, "diagnostic-decision", "reviewer", "approve", command.Request.Reason, 7, time.Now().UTC())
		return fmt.Errorf("real review lost response: preparation_valid=%t request_remaining=%s scope_remaining=%s context=%v: %w", preparationErr == nil, time.Until(request.Deadline), time.Until(scope.ExpiresAt), ctx.Err(), err)
	}
	cold, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cold.Close()
	repository.DB = cold
	retry := command
	retry.Request.Command.Request.RequestID = "renewed-review-request"
	retry.Request.Command.Scope.ScopeID = "renewed-review-scope"
	approved, err := repository.DecideReview(ctx, retry)
	if err != nil || approved.Decision != "approve" || approved.ContentHash != revision.ContentHash || approved.RowVersion != 1 || approved.PolicyVersion != 7 {
		return errors.New("cold review replay lost original decision")
	}
	q, current, err := load(revision)
	if err != nil || q.State != "approved" || q.RowVersion != 1 || current != string(approved.ID) {
		return errors.New("approve head not committed")
	}
	reviewContractSamples = append(reviewContractSamples, approvalSample(approved), reviewRequestSample(q))
	conflict := command
	conflict.Request.Reason = "changed reason"
	if result, err := repository.DecideReview(ctx, conflict); err == nil || result.ID != "" {
		return errors.New("review key accepted changed reason")
	}
	denied := repository
	denied.AuthorizeDecision = func(context.Context, document.ReviewCommand) error { return errors.New("review grant revoked") }
	if result, err := denied.DecideReview(ctx, command); err == nil || result.ID != "" {
		return errors.New("unauthorized review replay disclosed decision")
	}
	wrong := command
	wrong.DocumentID = "other-document"
	wrong.Request.Command.IdempotencyKey = "wrong-review-document"
	if result, err := repository.DecideReview(ctx, wrong); err == nil || result.ID != "" {
		return errors.New("review accepted foreign document")
	}
	revoke := makeCommand(revision, "revoke-original", "revoke", 1)
	revoked, err := repository.DecideReview(ctx, revoke)
	if err != nil || revoked.RowVersion != 2 || revoked.Decision != "revoke" || revoked.ID == approved.ID {
		return errors.New("review revoke did not append history")
	}
	q, current, err = load(revision)
	if err != nil || q.State != "revoked" || q.RowVersion != 2 || current != string(revoked.ID) {
		return errors.New("revoked approval remains current")
	}
	reviewContractSamples = append(reviewContractSamples, approvalSample(revoked), reviewRequestSample(q))
	// An old receipt is historical information, never proof of effective approval.
	old, err := repository.DecideReview(ctx, command)
	if err != nil || !reflect.DeepEqual(old, approved) {
		return errors.New("historical review receipt changed")
	}
	q, current, err = load(revision)
	if err != nil || q.State != "revoked" || current == string(old.ID) {
		return errors.New("historical replay reactivated approval")
	}
	if _, err := cold.ExecContext(ctx, `UPDATE akos_withdraw_v1.review_decisions SET payload=jsonb_set(payload,'{Reason}','"rewritten"') WHERE id=$1`, string(approved.ID)); err == nil {
		return errors.New("historical decision mutable")
	}
	for _, table := range []string{"review_receipts", "review_audit", "review_outbox"} {
		var count int
		if cold.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.`+table+` WHERE decision_id=$1`, string(approved.ID)).Scan(&count) != nil || count != 1 {
			return errors.New("review replay duplicated effects")
		}
	}
	var original []byte
	var historical contracts.Approval
	if cold.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.review_decisions WHERE id=$1`, string(approved.ID)).Scan(&original) != nil || json.Unmarshal(original, &historical) != nil || !reflect.DeepEqual(historical, approved) {
		return errors.New("revocation rewrote original approval")
	}
	fixture, err := newRevision("reject-review")
	if err != nil {
		return err
	}
	reject := makeCommand(fixture.Revision, "reject-review", "reject", 0)
	rejected, err := repository.DecideReview(ctx, reject)
	if err != nil || rejected.Decision != "reject" {
		return errors.New("reject did not commit")
	}
	q, _, err = load(fixture.Revision)
	if err != nil || q.State != "rejected" || q.RowVersion != 1 {
		return errors.New("rejected request head missing")
	}
	reviewContractSamples = append(reviewContractSamples, approvalSample(rejected), reviewRequestSample(q))
	for _, terminal := range []contracts.Revision{revision, fixture.Revision} {
		cmd := makeCommand(terminal, "terminal-"+string(terminal.ID), "approve", 1)
		if terminal.ID == revision.ID {
			cmd.Request.ExpectedApprovalVersion = 2
		}
		if result, err := repository.DecideReview(ctx, cmd); err == nil || result.ID != "" {
			return errors.New("terminal request silently reapproved")
		}
	}
	assertRollback := func(r contracts.Revision) error {
		q, current, err := load(r)
		if err != nil || q.State != "submitted" || q.RowVersion != 0 || current != "" {
			return errors.New("failed review advanced request")
		}
		var count int
		if cold.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.review_decisions WHERE revision_id=$1`, string(r.ID)).Scan(&count) != nil || count != 0 {
			return errors.New("failed review leaked decision")
		}
		return nil
	}
	for _, table := range []string{"review_audit", "review_outbox"} {
		fixture, err := newRevision("fail-" + table)
		if err != nil {
			return err
		}
		cmd := makeCommand(fixture.Revision, "fail-"+table, "approve", 0)
		if _, err := cold.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.fail_review_effect() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture review failure'; END $$`); err != nil {
			return err
		}
		if _, err := cold.ExecContext(ctx, `CREATE TRIGGER review_failure BEFORE INSERT ON akos_withdraw_v1.`+table+` FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fail_review_effect()`); err != nil {
			return err
		}
		result, reviewErr := repository.DecideReview(ctx, cmd)
		_, dropErr := cold.ExecContext(ctx, `DROP TRIGGER review_failure ON akos_withdraw_v1.`+table)
		if dropErr != nil {
			return dropErr
		}
		if !errors.Is(reviewErr, document.ErrReviewRejected) || result.ID != "" {
			return errors.New("review write failure not rolled back")
		}
		if err := assertRollback(fixture.Revision); err != nil {
			return err
		}
	}
	fixture, err = newRevision("missing-review-event")
	if err != nil {
		return err
	}
	missing := makeCommand(fixture.Revision, "missing-review-event", "approve", 0)
	if _, err := cold.ExecContext(ctx, `CREATE OR REPLACE FUNCTION akos_withdraw_v1.skip_review_effect() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`); err != nil {
		return err
	}
	if _, err := cold.ExecContext(ctx, `CREATE TRIGGER review_skip BEFORE INSERT ON akos_withdraw_v1.review_outbox FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.skip_review_effect()`); err != nil {
		return err
	}
	result, reviewErr := repository.DecideReview(ctx, missing)
	_, dropErr := cold.ExecContext(ctx, `DROP TRIGGER review_skip ON akos_withdraw_v1.review_outbox`)
	if dropErr != nil {
		return dropErr
	}
	if reviewErr == nil || result.ID != "" {
		return errors.New("missing review event passed commit gate")
	}
	if err := assertRollback(fixture.Revision); err != nil {
		return err
	}
	if _, err := repository.DecideReview(ctx, missing); err != nil {
		return errors.New("review could not retry confirmed rollback")
	}
	fixture, err = newRevision("revoked-review-authority")
	if err != nil {
		return err
	}
	cmd := makeCommand(fixture.Revision, "revoked-review-authority", "approve", 0)
	revoking := repository
	calls := 0
	revoking.AuthorizeDecision = func(c context.Context, cmd document.ReviewCommand) error {
		calls++
		if calls == 3 {
			return errors.New("authority revoked at commit")
		}
		return authorize(c, cmd)
	}
	if result, err := revoking.DecideReview(ctx, cmd); err == nil || result.ID != "" || calls != 3 {
		return errors.New("precommit reviewer revocation accepted")
	}
	if err := assertRollback(fixture.Revision); err != nil {
		return err
	}
	fixture, err = newRevision("race-review")
	if err != nil {
		return err
	}
	left := makeCommand(fixture.Revision, "race-review-approve", "approve", 0)
	right := makeCommand(fixture.Revision, "race-review-reject", "reject", 0)
	var wg sync.WaitGroup
	wins := make(chan bool, 2)
	for _, cmd := range []document.ReviewCommand{left, right} {
		wg.Add(1)
		go func(cmd document.ReviewCommand) {
			defer wg.Done()
			_, err := repository.DecideReview(ctx, cmd)
			wins <- err == nil
		}(cmd)
	}
	wg.Wait()
	close(wins)
	winners := 0
	for win := range wins {
		if win {
			winners++
		}
	}
	q, _, err = load(fixture.Revision)
	var count int
	if err != nil || winners != 1 || q.RowVersion != 1 || cold.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.review_decisions WHERE revision_id=$1`, string(fixture.Revision.ID)).Scan(&count) != nil || count != 1 {
		return errors.New("review race selected multiple decisions")
	}
	var inactive bool
	if cold.QueryRowContext(ctx, `SELECT publication_epoch=0 AND active_revision_id IS NULL AND active_generation_set_id IS NULL FROM akos_withdraw_v1.documents WHERE id=$1`, string(revision.DocumentID)).Scan(&inactive) != nil || !inactive {
		return errors.New("review decision published content")
	}
	return verifyPublicationStarts(ctx, db, scope, request, revision, approved, func(name string) (approvedPublicationFixture, error) {
		submitted, err := newRevision(name)
		if err != nil {
			return approvedPublicationFixture{}, err
		}
		cmd := makeCommand(submitted.Revision, "approve-"+name, "approve", 0)
		decision, err := repository.DecideReview(ctx, cmd)
		return approvedPublicationFixture{Revision: submitted.Revision, Approval: decision, Originals: originals}, err
	}, func(f approvedPublicationFixture) error {
		_, err := repository.DecideReview(ctx, makeCommand(f.Revision, "revoke-"+string(f.Revision.ID), "revoke", f.Approval.RowVersion))
		return err
	})
}

// Verification projections only, not public routes or authenticated DTOs.
func reviewRequestSample(q contracts.ApprovalRequest) map[string]any {
	return map[string]any{"schema": "ApprovalRequest", "payload": map[string]any{"id": q.ID, "revision_id": q.RevisionID, "document_id": q.DocumentID, "submitted_by": q.SubmittedBy, "submitted_at": q.SubmittedAt.UTC().Format(time.RFC3339Nano), "state": q.State, "row_version": q.RowVersion, "content_hash": q.ContentHash, "policy_version": q.PolicyVersion}}
}
func approvalSample(a contracts.Approval) map[string]any {
	return map[string]any{"schema": "Approval", "payload": map[string]any{"id": a.ID, "revision_id": a.RevisionID, "decision": a.Decision, "actor_id": a.ActorID, "reason": a.Reason, "content_hash": a.ContentHash, "policy_version": a.PolicyVersion, "row_version": a.RowVersion, "decided_at": a.DecidedAt.UTC().Format(time.RFC3339Nano)}}
}
