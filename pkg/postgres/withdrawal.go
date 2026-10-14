package postgres

import (
	"context"
	"database/sql"
	"math"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// WithdrawalPorts is a prototype transaction fragment, not a full UnitOfWork.
// Complete closes the current publication head and records its outbox event.
// Reserve creates the pending command in this transaction or reads its result.
type WithdrawalPorts = contracts.WithdrawalPorts

// RequestID is the trusted application request identifier, not a client role.
type WithdrawalCompletion = contracts.WithdrawalCompletion

type WithdrawalAdapter struct {
	DB     *sql.DB
	Verify VerifyScope
}

func (a WithdrawalAdapter) WithinWithdrawal(ctx context.Context, scope contracts.AccessScope, fn func(context.Context, contracts.WithdrawalPorts) error) error {
	return WithinWithdrawal(ctx, a.DB, scope, a.Verify, fn)
}

func WithinWithdrawal(ctx context.Context, db *sql.DB, scope contracts.AccessScope, verify VerifyScope, fn func(context.Context, WithdrawalPorts) error) error {
	if fn == nil || scope.Action != "publication.withdraw" {
		return ErrRepository
	}
	return withinSession(ctx, db, scope, verify, func(c context.Context, s *session) error {
		return fn(c, WithdrawalPorts{History: historyRepo{s}, Plans: planRepo{s}, Reserve: s.reserveWithdrawal, LoadCurrent: s.loadWithdrawalCurrent, Complete: s.completeWithdrawal})
	})
}

// The row version is an expected value, never a replacement supplied by clients.
// Any failure poisons this transaction, even if the callback ignores the error.
func (s *session) completeWithdrawal(ctx context.Context, scope contracts.AccessScope, completion WithdrawalCompletion) (err error) {
	defer func() {
		if err != nil {
			s.mu.Lock()
			s.failed = true
			s.mu.Unlock()
		}
	}()
	if completion.ExpectedRowVersion == 0 || completion.ExpectedRowVersion == math.MaxUint64 || completion.EventID == "" || completion.AuditID == "" || completion.RequestID == "" || scope.Action != "publication.withdraw" {
		return ErrRepository
	}
	p, err := (planRepo{s}).Load(ctx, scope, completion.PlanID)
	if err != nil {
		return err
	}
	h, err := (historyRepo{s}).Load(ctx, scope, p.HistoryID)
	if err != nil {
		return err
	}
	if err = s.enter(ctx, scope); err != nil {
		return err
	}
	defer s.mu.Unlock()
	reserved, ok := s.reservations[h.OperationID]
	if !ok || reserved.DocumentID != h.DocumentID || reserved.CommandDigest != h.CommandDigest {
		return ErrCommandPending
	}
	result, err := s.tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.documents SET publication_epoch=$1::numeric,row_version=row_version+1,active_revision_id=NULL,active_generation_set_id=NULL WHERE space_id=$2::text AND kb_id=$3::text AND id=$4::text AND publication_epoch=$5::numeric AND row_version=$6::numeric AND active_revision_id=$7::text AND active_generation_set_id=$8::text AND NOT deleted`, strconv.FormatUint(h.PublicationEpoch, 10), string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(p.DocumentID), strconv.FormatUint(p.PublicationEpoch, 10), strconv.FormatUint(completion.ExpectedRowVersion, 10), string(p.RevisionID), string(p.GenerationSetID))
	if err != nil {
		return ErrRepository
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return ErrRepository
	}
	_, err = s.tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.withdraw_outbox(id,space_id,kb_id,document_id,plan_id,history_id,operation_id,event_type,aggregate_version) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,'publication.withdrawn',$8::numeric)`, string(completion.EventID), string(p.SpaceID), string(p.KnowledgeBaseID), string(p.DocumentID), string(p.ID), string(h.ID), string(h.OperationID), strconv.FormatUint(h.PublicationEpoch, 10))
	if err != nil {
		return ErrRepository
	}
	actor, err := encodeActor(h.Actor)
	if err != nil {
		return ErrRepository
	}
	_, err = s.tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.withdraw_audit(id,request_id,space_id,kb_id,document_id,history_id,operation_id,action,actor_id,actor,occurred_at) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,'publication.withdraw',$8::text,$9::jsonb,$10::text)`, string(completion.AuditID), string(completion.RequestID), string(p.SpaceID), string(p.KnowledgeBaseID), string(p.DocumentID), string(h.ID), string(h.OperationID), string(h.ActorID), string(actor), h.OccurredAt.Format(timeFormat))
	if err != nil {
		return ErrRepository
	}
	result, err = s.tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.command_operations SET state='committed',history_id=$1::text WHERE space_id=$2::text AND kb_id=$3::text AND document_id=$4::text AND id=$5::text AND state='pending' AND actor_id=$6::text AND action='withdraw' AND command_digest=$7::text`, string(h.ID), string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(p.DocumentID), string(h.OperationID), string(scope.SubjectID), string(h.CommandDigest))
	if err != nil {
		return ErrRepository
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return ErrRepository
	}
	return nil
}
