package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

var deletionSamples []map[string]any
var deletedOriginalProbe func(context.Context) (map[string]any, error)

type lostDeleteResponse struct {
	store contracts.DocumentDeletionStore
}

func (p lostDeleteResponse) DeleteDocument(ctx context.Context, q contracts.DocumentDeletionCommand) (contracts.DeleteDocumentResult, error) {
	if _, e := p.store.DeleteDocument(ctx, q); e != nil {
		return contracts.DeleteDocumentResult{}, e
	}
	return contracts.DeleteDocumentResult{}, postgres.ErrCommitUnknown
}

func verifyDocumentDeletion(ctx context.Context, db *sql.DB, command contracts.CommandContext, transport *historicalTransport, cursor string) error {
	deadline, _ := ctx.Deadline()
	command.Request.Deadline, command.Scope.ExpiresAt = deadline, deadline
	command.Scope.Action = "document.delete"
	command.IdempotencyKey = "delete-history-document"
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.Action != "document.delete" || s.SubjectID != command.Principal.SubjectID || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("delete scope fixture denied")
		}
		return nil
	}
	authorizeCalls, denyAt := 0, 0
	authorize := func(c context.Context, q contracts.DeleteDocumentRequest) error {
		authorizeCalls++
		if authorizeCalls == denyAt || verify(c, q.Command.Scope) != nil || q.Command.Principal.SubjectID != command.Principal.SubjectID {
			return errors.New("delete resource fixture denied")
		}
		return nil
	}
	store := postgres.DocumentDeletions{DB: db, Verify: verify, AuthorizeDelete: authorize}
	service := document.DeletionService{Store: store, Verify: func(c context.Context, q contracts.DeleteDocumentRequest) error { return verify(c, q.Command.Scope) }}
	var version string
	if e := db.QueryRowContext(ctx, `SELECT row_version::text FROM akos_withdraw_v1.documents WHERE id=$1 AND NOT deleted`, historicalTarget.Document).Scan(&version); e != nil {
		return e
	}
	v, e := strconv.ParseUint(version, 10, 64)
	if e != nil {
		return e
	}
	request := contracts.DeleteDocumentRequest{Command: command, DocumentID: historicalTarget.Document, ExpectedDocumentVersion: v}
	unchanged := func() bool {
		var same bool
		return db.QueryRowContext(ctx, `SELECT NOT deleted AND row_version=$2::numeric AND publication_epoch=8 AND active_revision_id=$3 AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_receipts WHERE document_id=$1) AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.command_operations WHERE document_id=$1 AND action='delete') FROM akos_withdraw_v1.documents WHERE id=$1`, request.DocumentID, version, historicalTarget.UnpublishedRevision).Scan(&same) == nil && same
	}
	stale := request
	stale.ExpectedDocumentVersion--
	if _, e = service.Delete(ctx, stale); e == nil || !unchanged() {
		return errors.New("stale deletion CAS changed document")
	}
	// An actual outbox failure must undo history, plan, receipt and head together.
	if _, e = db.ExecContext(ctx, `CREATE FUNCTION akos_withdraw_v1.fixture_delete_event_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture delete event failure'; END; $$; CREATE TRIGGER fixture_delete_event_failure BEFORE INSERT ON akos_withdraw_v1.document_delete_outbox FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fixture_delete_event_failure()`); e != nil {
		return e
	}
	_, failure := service.Delete(ctx, request)
	if _, e = db.ExecContext(ctx, `DROP TRIGGER fixture_delete_event_failure ON akos_withdraw_v1.document_delete_outbox; DROP FUNCTION akos_withdraw_v1.fixture_delete_event_failure()`); e != nil {
		return e
	}
	if failure == nil || !unchanged() {
		return errors.New("delete event failure did not roll back")
	}
	// Even a successful INSERT that suppresses the audit cannot bypass the
	// deferred database delivery gate at commit.
	if _, e = db.ExecContext(ctx, `CREATE FUNCTION akos_withdraw_v1.fixture_delete_audit_skip() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END; $$; CREATE TRIGGER fixture_delete_audit_skip BEFORE INSERT ON akos_withdraw_v1.document_delete_audit FOR EACH ROW EXECUTE FUNCTION akos_withdraw_v1.fixture_delete_audit_skip()`); e != nil {
		return e
	}
	_, failure = service.Delete(ctx, request)
	if _, e = db.ExecContext(ctx, `DROP TRIGGER fixture_delete_audit_skip ON akos_withdraw_v1.document_delete_audit; DROP FUNCTION akos_withdraw_v1.fixture_delete_audit_skip()`); e != nil {
		return e
	}
	if failure == nil || !unchanged() {
		return errors.New("missing audit escaped deferred delete gate")
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.documents SET deleted=true,active_revision_id=NULL,active_generation_set_id=NULL,publication_epoch=publication_epoch+1,row_version=row_version+1 WHERE id=$1`, request.DocumentID); e == nil || !unchanged() {
		return errors.New("raw tombstone escaped deletion receipt gate")
	}
	authorizeCalls, denyAt = 0, 2
	if _, e = service.Delete(ctx, request); e == nil || !unchanged() {
		return errors.New("delete precommit revocation did not roll back")
	}
	authorizeCalls, denyAt = 0, 0
	lost := service
	lost.Store = lostDeleteResponse{store: store}
	if _, e = lost.Delete(ctx, request); !errors.Is(e, postgres.ErrCommitUnknown) {
		return errors.New("delete lost response fixture failed")
	}
	// A fresh adapter and scope must recover the immutable committed result.
	recovered := request
	recovered.Command.Request.RequestID = "delete-cold-recovery"
	recovered.Command.Scope.ScopeID = "delete-recovery-scope"
	cold := service
	cold.Store = postgres.DocumentDeletions{DB: db, Verify: verify, AuthorizeDelete: authorize}
	result, e := cold.Delete(ctx, recovered)
	if e != nil || result.PublicationEpoch != 9 || result.CleanupPlanID == nil {
		return errors.New("delete cold receipt recovery failed")
	}
	replay, e := cold.Delete(ctx, recovered)
	if e != nil || !reflect.DeepEqual(result, replay) {
		return errors.New("delete replay changed committed receipt")
	}
	conflict := recovered
	conflict.ExpectedDocumentVersion++
	if _, e = cold.Delete(ctx, conflict); !errors.Is(e, postgres.ErrCommandConflict) {
		return errors.New("delete semantic key conflict accepted")
	}
	if e = transport.deny("deleted-old-cursor", cursor, false); e != nil {
		return e
	}
	if e = transport.deny("deleted-fresh-request", "", false); e != nil {
		return e
	}
	out, e := readLifecyclePage(ctx, historicalTarget.UnpublishedRevision, "")
	if e == nil || len(out.Record.Payload) != 0 || out.NextCursor != "" {
		return errors.New("deleted current version returned content")
	}
	if _, e = publishHistoricalAgain(ctx, historicalTarget.NewRevision, historicalTarget.NewApproval, 9, "deleted-republish"); e == nil {
		return errors.New("deleted document republished")
	}
	if _, e = db.ExecContext(ctx, `UPDATE akos_withdraw_v1.documents SET deleted=false WHERE id=$1`, request.DocumentID); e == nil {
		return errors.New("document tombstone could be undone")
	}
	original, e := deletedOriginalProbe(ctx)
	if e != nil {
		return e
	}
	fetch := func(query string, args ...any) (json.RawMessage, error) {
		var raw []byte
		e := db.QueryRowContext(ctx, query, args...).Scan(&raw)
		return append(json.RawMessage(nil), raw...), e
	}
	job, e := fetch(`SELECT row_to_json(j) FROM akos_withdraw_v1.document_delete_jobs j WHERE id=$1`, result.JobID)
	if e != nil {
		return e
	}
	head, e := fetch(`SELECT row_to_json(d) FROM akos_withdraw_v1.documents d WHERE id=$1`, request.DocumentID)
	if e != nil {
		return e
	}
	history, e := fetch(`SELECT row_to_json(h) FROM akos_withdraw_v1.publication_history h WHERE id=$1`, result.HistoryID)
	if e != nil {
		return e
	}
	receipt, e := fetch(`SELECT row_to_json(r) FROM akos_withdraw_v1.document_delete_receipts r WHERE operation_id=$1`, result.OperationID)
	if e != nil {
		return e
	}
	event, e := fetch(`SELECT row_to_json(e) FROM akos_withdraw_v1.document_delete_outbox e WHERE operation_id=$1`, result.OperationID)
	if e != nil {
		return e
	}
	audit, e := fetch(`SELECT row_to_json(a) FROM akos_withdraw_v1.document_delete_audit a WHERE operation_id=$1`, result.OperationID)
	if e != nil {
		return e
	}
	plan, e := fetch(`SELECT row_to_json(p) FROM akos_withdraw_v1.cleanup_plans p WHERE id=$1`, *result.CleanupPlanID)
	if e != nil {
		return e
	}
	targets, e := fetch(`SELECT jsonb_agg(t ORDER BY t.id) FROM akos_withdraw_v1.cleanup_targets t WHERE plan_id=$1`, *result.CleanupPlanID)
	if e != nil {
		return e
	}
	generations, e := fetch(`SELECT jsonb_agg(g ORDER BY g.id) FROM akos_withdraw_v1.index_generations g JOIN akos_withdraw_v1.cleanup_plans p ON p.generation_set_id=g.generation_set_id WHERE p.id=$1`, *result.CleanupPlanID)
	if e != nil {
		return e
	}
	var retained, planTargets int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.revision_contents WHERE document_id=$1`, request.DocumentID).Scan(&retained); e != nil || retained != 3 {
		return errors.New("deletion removed retained revisions")
	}
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.cleanup_targets WHERE plan_id=$1`, *result.CleanupPlanID).Scan(&planTargets); e != nil || planTargets < 1 {
		return errors.New("deletion lacks fixed cleanup targets")
	}
	deletionSamples = append(deletionSamples, map[string]any{"job": job, "plan": plan, "targets": targets, "generations": generations, "missing_audit_commit_rejected": true, "raw_tombstone_commit_rejected": true, "case": "published", "request": request, "result": result, "head": head, "history": history, "receipt": receipt, "event": event, "audit": audit, "retained_revision_count": retained, "cleanup_target_count": planTargets, "original": original, "transport_denials": transport.samples[len(transport.samples)-2:], "stale_cas_rejected": true, "event_failure_rolled_back": true, "precommit_revocation_rolled_back": true, "cold_recovery": true, "replay_no_duplicate": true, "semantic_conflict_rejected": true, "current_read_rejected": true, "republish_rejected": true, "tombstone_undo_rejected": true, "governance_authority": "fixture"})
	// Never-published and already-withdrawn documents have no active source and
	// must not fabricate a cleanup plan or revision/set pair.
	if _, e = db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.documents(space_id,kb_id,id,publication_epoch,row_version) VALUES('space','kb','delete-unpublished',0,1)`); e != nil {
		return e
	}
	var withdrawnID contracts.ID
	if e = db.QueryRowContext(ctx, `SELECT d.id FROM akos_withdraw_v1.documents d JOIN akos_withdraw_v1.publication_history h ON h.document_id=d.id AND h.publication_epoch=d.publication_epoch WHERE d.space_id='space' AND d.kb_id='kb' AND NOT d.deleted AND d.active_revision_id IS NULL AND h.action='withdraw' ORDER BY d.id LIMIT 1`).Scan(&withdrawnID); e != nil {
		return e
	}
	for _, fixture := range []struct {
		name string
		id   contracts.ID
	}{{"unpublished", "delete-unpublished"}, {"withdrawn", withdrawnID}} {
		q := request
		q.DocumentID = fixture.id
		q.Command.IdempotencyKey = "delete-" + fixture.name
		if e = db.QueryRowContext(ctx, `SELECT row_version::text FROM akos_withdraw_v1.documents WHERE id=$1`, q.DocumentID).Scan(&version); e != nil {
			return e
		}
		q.ExpectedDocumentVersion, _ = strconv.ParseUint(version, 10, 64)
		deleted, e := service.Delete(ctx, q)
		if e != nil || deleted.CleanupPlanID != nil {
			return errors.New("inactive document deletion failed")
		}
		h, e := fetch(`SELECT row_to_json(h) FROM akos_withdraw_v1.publication_history h WHERE id=$1`, deleted.HistoryID)
		if e != nil {
			return e
		}
		d, e := fetch(`SELECT row_to_json(d) FROM akos_withdraw_v1.documents d WHERE id=$1`, q.DocumentID)
		if e != nil {
			return e
		}
		job, e := fetch(`SELECT row_to_json(j) FROM akos_withdraw_v1.document_delete_jobs j WHERE id=$1`, deleted.JobID)
		if e != nil {
			return e
		}
		var previous json.RawMessage
		if fixture.name == "withdrawn" {
			previous, e = fetch(`SELECT row_to_json(h) FROM akos_withdraw_v1.publication_history h WHERE document_id=$1 AND publication_epoch=$2::numeric`, q.DocumentID, strconv.FormatUint(deleted.PublicationEpoch-1, 10))
			if e != nil {
				return e
			}
		}
		deletionSamples = append(deletionSamples, map[string]any{"job": job, "previous_history": previous, "case": fixture.name, "request": q, "result": deleted, "history": h, "head": d, "governance_authority": "fixture"})
	}
	return runManagementDeletionProbe(ctx)
}
