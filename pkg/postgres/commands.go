package postgres

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrCommandConflict = errors.New("withdrawal idempotency key conflicts")
var ErrCommandPending = errors.New("withdrawal command requires reconciliation")

// Digests are trusted application commitments, not hashes of transient scope
// tokens. Their canonical encoding and identity provenance are separate gates.
type WithdrawalCommand = contracts.WithdrawalCommand
type CommandReservation = contracts.CommandReservation

func commandID(v string) bool {
	return utf8.ValidString(v) && utf8.RuneCountInString(v) >= 1 && utf8.RuneCountInString(v) <= 200
}
func commandDigest(v contracts.Digest) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// New reservation and all withdrawal writes share the callback transaction.
// Existing pending commands cannot be taken over by a retry.
func (s *session) reserveWithdrawal(ctx context.Context, scope contracts.AccessScope, command WithdrawalCommand) (reservation CommandReservation, err error) {
	defer func() {
		if err != nil {
			s.mu.Lock()
			s.failed = true
			s.mu.Unlock()
		}
	}()
	if scope.Action != "publication.withdraw" || !commandID(string(command.OperationID)) || !commandID(string(command.DocumentID)) || !commandID(command.Key) || !commandDigest(command.ActorSemanticDigest) || !commandDigest(command.CommandDigest) {
		return reservation, ErrRepository
	}
	if err = s.enter(ctx, scope); err != nil {
		return reservation, err
	}
	// Keep all SQL in a locked block; history loading uses the same session gate.
	var created bool
	var operation, doc, kb, actor, digest, state string
	var history sql.NullString
	func() {
		defer s.mu.Unlock()
		result, e := s.tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.command_operations(id,space_id,kb_id,document_id,actor_semantic_digest,actor_id,action,idempotency_key,command_digest,state,history_id) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,'withdraw',$7::text,$8::text,'pending',NULL) ON CONFLICT(space_id,actor_semantic_digest,action,idempotency_key) DO NOTHING`, string(command.OperationID), string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(command.DocumentID), string(command.ActorSemanticDigest), string(scope.SubjectID), command.Key, string(command.CommandDigest))
		if e != nil {
			err = ErrRepository
			return
		}
		n, e := result.RowsAffected()
		if e != nil {
			err = ErrRepository
			return
		}
		created = n == 1
		e = s.tx.QueryRowContext(ctx, `SELECT id,document_id,kb_id,actor_id,command_digest,state,history_id FROM akos_withdraw_v1.command_operations WHERE space_id=$1::text AND actor_semantic_digest=$2::text AND action='withdraw' AND idempotency_key=$3::text FOR UPDATE`, string(scope.SpaceID), string(command.ActorSemanticDigest), command.Key).Scan(&operation, &doc, &kb, &actor, &digest, &state, &history)
		if e != nil {
			err = ErrRepository
		}
	}()
	if err != nil {
		return reservation, err
	}
	if doc != string(command.DocumentID) || kb != string(scope.KnowledgeBaseIDs[0]) || actor != string(scope.SubjectID) || digest != string(command.CommandDigest) {
		return reservation, ErrCommandConflict
	}
	reservation.OperationID = contracts.ID(operation)
	if created {
		s.mu.Lock()
		if s.closed || s.failed {
			s.mu.Unlock()
			return CommandReservation{}, ErrRepository
		}
		if s.reservations == nil {
			s.reservations = make(map[contracts.ID]WithdrawalCommand)
		}
		s.reservations[reservation.OperationID] = command
		s.mu.Unlock()
		reservation.Created = true
		return reservation, nil
	}
	if state != "committed" || !history.Valid {
		return reservation, ErrCommandPending
	}
	h, e := (historyRepo{s}).Load(ctx, scope, contracts.ID(history.String))
	if e != nil {
		return CommandReservation{}, e
	}
	if h.OperationID != reservation.OperationID || h.DocumentID != command.DocumentID || h.Action != "withdraw" || h.ActorID != scope.SubjectID || h.CommandDigest != command.CommandDigest {
		return CommandReservation{}, ErrRepository
	}
	reservation.HistoryID, reservation.PublicationEpoch = h.ID, h.PublicationEpoch
	return reservation, nil
}
