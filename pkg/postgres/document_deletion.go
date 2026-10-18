package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// DocumentDeletions implements logical deletion, never original-byte purge.
// Resource authorization and scope verification are required independently.
type DocumentDeletions struct {
	DB              *sql.DB
	Verify          VerifyScope
	AuthorizeDelete func(context.Context, contracts.DeleteDocumentRequest) error
}

func (p DocumentDeletions) DeleteDocument(ctx context.Context, input contracts.DocumentDeletionCommand) (contracts.DeleteDocumentResult, error) {
	var empty contracts.DeleteDocumentResult
	q, err := copyQuery(input)
	if err != nil || p.AuthorizeDelete == nil || !commandID(string(q.Request.Command.Request.RequestID)) || q.OccurredAt.IsZero() || q.OccurredAt.Year() < 1 || q.OccurredAt.Year() > 9999 {
		return empty, ErrRepository
	}
	for _, id := range []contracts.ID{q.OperationID, q.HistoryID, q.PlanID, q.AuditID, q.EventID, q.JobID} {
		if !commandID(string(id)) {
			return empty, ErrRepository
		}
	}
	if ctx == nil || q.Request.Command.Request.Deadline.IsZero() {
		return empty, ErrRepository
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Command.Request.Deadline)
	defer cancel()
	actor, digest, e := document.DeletionCommitments(q.Request)
	if e != nil || actor != q.ActorDigest || digest != q.CommandDigest {
		return empty, ErrRepository
	}
	command := q.Request.Command
	authorize := func(c context.Context) error {
		owned, e := copyQuery(q.Request)
		if e != nil {
			return ErrRepository
		}
		return p.AuthorizeDelete(c, owned)
	}
	var result contracts.DeleteDocumentResult
	err = withinSession(ctx, p.DB, command.Scope, p.Verify, func(c context.Context, s *session) error {
		if e := authorize(c); e != nil {
			return e
		}
		reservation, e := s.reserveCommand(c, command.Scope, WithdrawalCommand{OperationID: q.OperationID, DocumentID: q.Request.DocumentID, Key: command.IdempotencyKey, ActorSemanticDigest: q.ActorDigest, CommandDigest: q.CommandDigest}, "document.delete", "delete")
		if e != nil {
			if errors.Is(e, ErrCommandConflict) {
				return errors.Join(e, contracts.ErrDocumentDeleteConflict)
			}
			return e
		}
		if !reservation.Created {
			if e = s.enter(c, command.Scope); e != nil {
				return e
			}
			var raw []byte
			e = s.tx.QueryRowContext(c, `SELECT result FROM akos_withdraw_v1.document_delete_receipts WHERE operation_id=$1 AND history_id=$2 AND space_id=$3 AND kb_id=$4 AND document_id=$5`, reservation.OperationID, reservation.HistoryID, command.Scope.SpaceID, command.Scope.KnowledgeBaseIDs[0], q.Request.DocumentID).Scan(&raw)
			s.mu.Unlock()
			if e != nil || json.Unmarshal(raw, &result) != nil || result.OperationID != reservation.OperationID || result.HistoryID != reservation.HistoryID || result.DocumentID != q.Request.DocumentID || result.DocumentVersion != q.Request.ExpectedDocumentVersion+1 || result.PublicationEpoch != reservation.PublicationEpoch {
				return ErrRepository
			}
			return authorize(c)
		}
		if e = s.enter(c, command.Scope); e != nil {
			return e
		}
		var epoch, version string
		var revision, set sql.NullString
		var deleted bool
		e = s.tx.QueryRowContext(c, `SELECT publication_epoch::text,row_version::text,active_revision_id,active_generation_set_id,deleted FROM akos_withdraw_v1.documents WHERE space_id=$1 AND kb_id=$2 AND id=$3 FOR UPDATE`, command.Scope.SpaceID, command.Scope.KnowledgeBaseIDs[0], q.Request.DocumentID).Scan(&epoch, &version, &revision, &set, &deleted)
		s.mu.Unlock()
		if e != nil {
			return ErrRepository
		}
		if deleted {
			return contracts.ErrDocumentDeleteConflict
		}
		currentEpoch, e := strconv.ParseUint(epoch, 10, 64)
		if e != nil || currentEpoch == math.MaxUint64 {
			return ErrRepository
		}
		currentVersion, e := strconv.ParseUint(version, 10, 64)
		if e == nil && currentVersion != q.Request.ExpectedDocumentVersion {
			return contracts.ErrDocumentDeleteConflict
		}
		if e != nil || revision.Valid != set.Valid {
			return ErrRepository
		}
		history := contracts.PublicationHistory{ID: q.HistoryID, OperationID: q.OperationID, SpaceID: command.Scope.SpaceID, KnowledgeBaseID: command.Scope.KnowledgeBaseIDs[0], DocumentID: q.Request.DocumentID, PreviousEpoch: currentEpoch, PublicationEpoch: currentEpoch + 1, Action: "delete", PreviousRevisionID: idPointer(revision), PreviousGenerationSetID: idPointer(set), ActorID: command.Principal.SubjectID, Actor: command.Principal, Reason: "logical document deletion", OccurredAt: q.OccurredAt, CommandDigest: q.CommandDigest}
		if e = (historyRepo{s}).Append(c, command.Scope, history); e != nil {
			return e
		}
		var planID *contracts.ID
		if revision.Valid {
			snapshot, e := s.loadWithdrawalCurrent(c, command.Scope, q.Request.DocumentID)
			if e != nil {
				return e
			}
			plan := contracts.CleanupPlan{ID: q.PlanID, HistoryID: q.HistoryID, OperationID: q.OperationID, SpaceID: history.SpaceID, KnowledgeBaseID: history.KnowledgeBaseID, DocumentID: history.DocumentID, RevisionID: snapshot.RevisionID, GenerationSetID: snapshot.GenerationSetID, PublicationEpoch: currentEpoch, ProfileDigest: command.Request.ProfileDigest, Targets: snapshot.Targets, CreatedAt: q.OccurredAt}
			if e = (planRepo{s}).Append(c, command.Scope, plan); e != nil {
				return e
			}
			planID = &q.PlanID
		}
		if e = s.enter(c, command.Scope); e != nil {
			return e
		}
		defer s.mu.Unlock()
		changed, e := s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.documents SET deleted=true,active_revision_id=NULL,active_generation_set_id=NULL,publication_epoch=$1::numeric,row_version=row_version+1 WHERE space_id=$2 AND kb_id=$3 AND id=$4 AND NOT deleted AND publication_epoch=$5::numeric AND row_version=$6::numeric AND active_revision_id IS NOT DISTINCT FROM $7::text AND active_generation_set_id IS NOT DISTINCT FROM $8::text`, strconv.FormatUint(history.PublicationEpoch, 10), history.SpaceID, history.KnowledgeBaseID, history.DocumentID, epoch, version, nullableID(history.PreviousRevisionID), nullableID(history.PreviousGenerationSetID))
		if e != nil {
			return ErrRepository
		}
		if n, e := changed.RowsAffected(); e != nil || n != 1 {
			return ErrRepository
		}
		result = contracts.DeleteDocumentResult{JobID: q.JobID, OperationID: q.OperationID, HistoryID: q.HistoryID, DocumentID: history.DocumentID, DocumentVersion: currentVersion + 1, PublicationEpoch: history.PublicationEpoch, CleanupPlanID: planID, DeletedAt: q.OccurredAt}
		job := contracts.Job{ID: q.JobID, SpaceID: history.SpaceID, OperationID: q.OperationID, Stage: "document.delete.cleanup", ProfileDigest: command.Request.ProfileDigest, State: "queued", RowVersion: 1}
		jobRaw, e := json.Marshal(job)
		if e != nil {
			return ErrRepository
		}
		_, e = s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.document_delete_jobs(id,operation_id,history_id,space_id,kb_id,document_id,profile_digest,job) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, q.JobID, q.OperationID, q.HistoryID, history.SpaceID, history.KnowledgeBaseID, history.DocumentID, command.Request.ProfileDigest, string(jobRaw))
		if e != nil {
			return ErrRepository
		}
		_, e = s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.document_delete_job_targets(job_id,plan_id,target_id) SELECT $1,p.id,t.id FROM akos_withdraw_v1.cleanup_plans p JOIN akos_withdraw_v1.cleanup_targets t ON t.plan_id=p.id WHERE p.space_id=$2 AND p.kb_id=$3 AND p.document_id=$4 AND p.publication_epoch<$5::numeric`, q.JobID, history.SpaceID, history.KnowledgeBaseID, history.DocumentID, strconv.FormatUint(history.PublicationEpoch, 10))
		if e != nil {
			return ErrRepository
		}
		_, e = s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.document_delete_job_progress SET total_targets=(SELECT count(*) FROM akos_withdraw_v1.document_delete_job_targets WHERE job_id=$1) WHERE job_id=$1`, q.JobID)
		if e != nil {
			return ErrRepository
		}
		raw, e := json.Marshal(result)
		if e != nil {
			return ErrRepository
		}
		_, e = s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.document_delete_receipts(operation_id,history_id,space_id,kb_id,document_id,expected_version,result_version,publication_epoch,plan_id,event_id,audit_id,result,job_id) VALUES($1,$2,$3,$4,$5,$6::numeric,$7::numeric,$8::numeric,$9,$10,$11,$12::jsonb,$13)`, q.OperationID, q.HistoryID, history.SpaceID, history.KnowledgeBaseID, history.DocumentID, version, strconv.FormatUint(result.DocumentVersion, 10), strconv.FormatUint(result.PublicationEpoch, 10), nullableID(planID), q.EventID, q.AuditID, string(raw), q.JobID)
		if e != nil {
			return ErrRepository
		}
		_, e = s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.document_delete_outbox VALUES($1,$2,$3,$4,$5,$6,'document.deleted',$7::numeric)`, q.EventID, history.SpaceID, history.KnowledgeBaseID, history.DocumentID, q.HistoryID, q.OperationID, strconv.FormatUint(history.PublicationEpoch, 10))
		if e != nil {
			return ErrRepository
		}
		actorRaw, e := encodeActor(history.Actor)
		if e != nil {
			return e
		}
		_, e = s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.document_delete_audit VALUES($1,$2,$3,$4,$5,$6,$7,'document.delete',$8,$9::jsonb,$10)`, q.AuditID, command.Request.RequestID, history.SpaceID, history.KnowledgeBaseID, history.DocumentID, q.HistoryID, q.OperationID, history.ActorID, string(actorRaw), history.OccurredAt.UTC().Format(timeFormat))
		if e != nil {
			return ErrRepository
		}
		changed, e = s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.command_operations SET state='committed',history_id=$1 WHERE id=$2 AND state='pending' AND action='delete' AND command_digest=$3 AND actor_id=$4`, q.HistoryID, q.OperationID, q.CommandDigest, history.ActorID)
		if e != nil {
			return ErrRepository
		}
		if n, e := changed.RowsAffected(); e != nil || n != 1 {
			return ErrRepository
		}
		return authorize(c)
	})
	if err != nil {
		return empty, err
	}
	return result, nil
}

var _ contracts.DocumentDeletionStore = DocumentDeletions{}
