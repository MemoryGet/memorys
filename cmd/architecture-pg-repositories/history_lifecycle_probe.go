package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/withdrawal"
)

var readLifecyclePage func(context.Context, contracts.ID, string) (knowledge.ReadPageResult, error)
var releaseHistoricalApp func()
var historicalAppProfile contracts.Digest

var historicalLifecycleSamples []map[string]any

// Independent bounded fixture phase. Governance grants remain fixtures; reader
// grants are newly sealed native ACLs, never a rollback to an earlier policy.
func verifyHistoricalLifecycle(db *sql.DB, command contracts.CommandContext) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	command.Request.Deadline, command.Scope.ExpiresAt = deadline, deadline
	if publishHistoricalAgain == nil || historicalReadSource == nil {
		return errors.New("history lifecycle fixture missing")
	}
	who := historicalReadSource.request.Principal
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`INSERT INTO akos_withdraw_v1.access_policy_snapshots VALUES('space',3,false)`,
		`INSERT INTO akos_withdraw_v1.access_policy_grants VALUES('space',3,'fixture','reader','identity-client','kb','knowledge.read'),('space',3,'fixture','reader','identity-client','kb','knowledge.history.read')`,
		`UPDATE akos_withdraw_v1.access_policy_snapshots SET sealed=true WHERE space_id='space' AND version=3`,
		`UPDATE akos_withdraw_v1.access_policy_heads SET version=3 WHERE space_id='space' AND version=2`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	authority := postgres.Authorizer{DB: db, ScopeLifetime: time.Minute}
	request := historicalReadSource.request
	request.Base.RevisionID = historicalTarget.NewRevision
	request.Base.Request.Deadline = deadline
	request.Base.Request.RequestID = "history-lifecycle-read"
	for _, action := range []contracts.Action{"knowledge.read", contracts.HistoricalReadAction} {
		scope, e := authority.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: action, SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, ResourceIDs: []contracts.ID{historicalTarget.Document}})
		if e != nil {
			return e
		}
		if action == "knowledge.read" {
			request.Base.Scope = scope
		} else {
			request.HistoryScope = scope
		}
	}
	reads := postgres.QueryReads{DB: db, Verify: func(c context.Context, s contracts.AccessScope) error {
		fresh, e := authority.Authorize(c, contracts.AuthorizeRequest{Principal: who, Action: s.Action, SpaceID: s.SpaceID, KnowledgeBaseIDs: s.KnowledgeBaseIDs, ResourceIDs: []contracts.ID{historicalTarget.Document}})
		if e != nil || fresh.PolicyVersion != s.PolicyVersion || s.SubjectID != who.SubjectID {
			return postgres.ErrAuthorization
		}
		return nil
	}}
	source := postgres.HistoricalFrozenReader{Reads: reads, Authorizer: authority}
	publish := func(revision, approval contracts.ID, epoch uint64) error {
		_, e := publishHistoricalAgain(ctx, revision, approval, epoch, fmt.Sprintf("history-lifecycle-%d", epoch+1))
		return e
	}
	// Normal replacement preserves the prior approved version.
	if err = publish(historicalTarget.UnpublishedRevision, historicalTarget.UnpublishedApproval.ID, 2); err != nil {
		return err
	}
	before, err := source.ReadHistoricalContent(ctx, request)
	if err != nil {
		return fmt.Errorf("history before withdrawal: %w", err)
	}
	firstPage, err := readLifecyclePage(ctx, historicalTarget.NewRevision, "")
	if err != nil || firstPage.NextCursor == "" || len(firstPage.BlockIDs) != 1 {
		return fmt.Errorf("lifecycle application first page: error=%v blocks=%d cursor_present=%t", err, len(firstPage.BlockIDs), firstPage.NextCursor != "")
	}
	transport, err := startHistoricalTransport(ctx)
	if err != nil {
		return err
	}
	defer transport.server.Close()
	transportCursor, err := transport.pair("before")
	if err != nil {
		return err
	}
	oldCursor := firstPage.NextCursor
	lastPage, err := readLifecyclePage(ctx, historicalTarget.NewRevision, oldCursor)
	if err != nil || lastPage.NextCursor != "" || len(lastPage.BlockIDs) != 1 {
		return errors.New("lifecycle application continuation failed")
	}
	if err = publish(historicalTarget.NewRevision, historicalTarget.NewApproval, 3); err != nil {
		return err
	}
	command.Scope.Action = "publication.withdraw"
	command.IdempotencyKey = "history-lifecycle-withdraw"
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SpaceID != "space" || s.Action != "publication.withdraw" || s.SubjectID != command.Principal.SubjectID {
			return errors.New("lifecycle withdrawal fixture denied")
		}
		return nil
	}
	service := withdrawal.Service{Transactions: postgres.WithdrawalAdapter{DB: db, Verify: verify}, Prepare: (withdrawal.Preparer{Verify: func(c context.Context, q contracts.WithdrawRequest) error { return verify(c, q.Command.Scope) }}).Prepare}
	withdrawn, err := service.Withdraw(ctx, contracts.WithdrawRequest{Command: command, DocumentID: historicalTarget.Document, ExpectedEpoch: 4, Reason: "explicit lifecycle withdrawal"})
	if err != nil {
		return err
	}
	denied := func() bool {
		record, e := source.ReadHistoricalContent(ctx, request)
		return e != nil && len(record.Content.Payload) == 0 && record.PublicationID == ""
	}
	if !denied() {
		return errors.New("withdrawn document exposed historical content")
	}
	denyApplication := func(cursor string, cursorError bool) bool {
		out, e := readLifecyclePage(ctx, historicalTarget.NewRevision, cursor)
		return e != nil && (!cursorError || errors.Is(e, knowledge.ErrReadCursor)) && len(out.Record.Payload) == 0 && out.NextCursor == "" && len(out.BlockIDs) == 0
	}
	if !denyApplication(oldCursor, false) || !denyApplication("", false) {
		return errors.New("withdrawn application exposed content")
	}
	if err = transport.deny("withdrawn", transportCursor, false); err != nil {
		return err
	}
	// Publishing another version must not undo the explicit target withdrawal.
	if err = publish(historicalTarget.UnpublishedRevision, historicalTarget.UnpublishedApproval.ID, 5); err != nil {
		return err
	}
	if !denied() {
		return errors.New("withdrawn target restored by unrelated publication")
	}
	if !denyApplication(oldCursor, false) || !denyApplication("", false) {
		return errors.New("withdrawn historical application target restored by other publish")
	}
	if err = transport.deny("other-version", transportCursor, false); err != nil {
		return err
	}
	// Only explicitly republishing the target establishes a later valid history.
	if err = publish(historicalTarget.NewRevision, historicalTarget.NewApproval, 6); err != nil {
		return err
	}
	if err = publish(historicalTarget.UnpublishedRevision, historicalTarget.UnpublishedApproval.ID, 7); err != nil {
		return err
	}
	after, err := source.ReadHistoricalContent(ctx, request)
	if err != nil {
		return fmt.Errorf("history after explicit republication: %w", err)
	}
	if before.PublicationEpoch != 2 || after.PublicationEpoch != 7 || after.Current.PublicationEpoch != 8 || before.Review.Approval.ID != after.Review.Approval.ID || string(before.Content.Payload) != string(after.Content.Payload) {
		return errors.New("historical republication binding differs")
	}
	if !denyApplication(oldCursor, true) {
		return errors.New("old application cursor revived after republication")
	}
	freshPage, err := readLifecyclePage(ctx, historicalTarget.NewRevision, "")
	if err != nil || freshPage.NextCursor == "" || freshPage.NextCursor == oldCursor {
		return errors.New("fresh historical application cursor missing")
	}
	freshLast, err := readLifecyclePage(ctx, historicalTarget.NewRevision, freshPage.NextCursor)
	if err != nil || freshLast.NextCursor != "" || len(freshLast.BlockIDs) != 1 {
		return errors.New("fresh historical application continuation failed")
	}
	if err = transport.deny("republished-old-cursor", transportCursor, true); err != nil {
		return err
	}
	freshTransportCursor, err := transport.pair("after")
	if err != nil || freshTransportCursor == transportCursor {
		return errors.New("history lifecycle fresh transport cursor failed")
	}
	rows, err := db.QueryContext(ctx, `SELECT row_to_json(h) FROM akos_withdraw_v1.publication_history h WHERE document_id=$1 ORDER BY publication_epoch`, historicalTarget.Document)
	if err != nil {
		return err
	}
	var history []json.RawMessage
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		history = append(history, append(json.RawMessage(nil), raw...))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	grants, err := historicalGrantRows(ctx, db, request)
	if err != nil {
		return err
	}
	historicalLifecycleSamples = append(historicalLifecycleSamples, map[string]any{"transport_samples": transport.samples, "application_profile_digest": historicalAppProfile, "application_before_first": firstPage, "application_before_last": lastPage, "application_after_first": freshPage, "application_after_last": freshLast, "application_withdrawn_rejected": true, "application_withdrawn_after_other_publish_rejected": true, "application_old_cursor_rejected_after_republish": true, "application_identity": "rs256-postgres-status", "request": request, "before": before, "after": after, "withdrawal": withdrawn, "history": history, "stored_grants": grants, "withdrawn_document_rejected": true, "withdrawn_target_after_other_publish_rejected": true, "governance_authority": "fixture", "reader_authority": "postgres-sealed-acl"})
	return verifyDocumentDeletion(ctx, db, command, transport, freshTransportCursor)
}
