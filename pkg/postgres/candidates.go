package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// Candidates implements only synchronous candidate persistence. Verify must
// authorize the current actor's candidate write, not merely validate a token.
// It is not a task lease consumer or the full metadata UnitOfWork provider.
type Candidates struct {
	DB     *sql.DB
	Verify VerifyScope
	// AuthorizeCandidate checks the specific document/draft/original write.
	// Scope validity alone must never grant every resource in the knowledge base.
	AuthorizeCandidate func(context.Context, document.CandidateCommand) error
}

var _ document.CandidateRepository = Candidates{}

func (p Candidates) CommitCandidate(ctx context.Context, command document.CandidateCommand) (document.CandidateReceipt, error) {
	receipt, err := p.commitCandidate(ctx, command)
	if err == nil {
		return receipt, nil
	}
	if errors.Is(err, ErrCommitUnknown) || errors.Is(err, document.ErrCandidateCommitUnknown) {
		return document.CandidateReceipt{}, errors.Join(ErrCommitUnknown, document.ErrCandidateCommitUnknown)
	}
	return document.CandidateReceipt{}, errors.Join(document.ErrCandidateRejected, err)
}

func (p Candidates) commitCandidate(ctx context.Context, command document.CandidateCommand) (document.CandidateReceipt, error) {
	var receipt document.CandidateReceipt
	if p.AuthorizeCandidate == nil {
		return receipt, ErrRepository
	}
	raw, err := json.Marshal(command)
	if err != nil || len(raw) > 12<<20 {
		return receipt, ErrRepository
	}
	var frozen document.CandidateCommand
	if json.Unmarshal(raw, &frozen) != nil {
		return receipt, ErrRepository
	}
	command = frozen
	if command.Scope.Action != "document.parse" || command.DocumentID == "" || command.CandidateID == "" || command.Request.RequestID == "" || command.Request.Deadline.IsZero() || command.Request.ProfileDigest != command.Candidate.ProfileDigest {
		return receipt, ErrRepository
	}
	// Validate the platform candidate before beginning SQL. Actual application is
	// computed again from the locked authoritative draft below.
	validation := contracts.Draft{ID: command.Candidate.DraftID, DocumentID: command.DocumentID, State: "draft", RowVersion: command.Candidate.ExpectedVersion, Blobs: []contracts.BlobRef{command.Candidate.Input}}
	if _, err := document.MergeParsedCandidate(validation, command.Candidate); err != nil {
		return receipt, ErrRepository
	}
	if ctx == nil {
		return receipt, ErrRepository
	}
	if _, ok := ctx.Deadline(); !ok {
		return receipt, ErrRepository
	}
	ctx, cancel := context.WithDeadline(ctx, command.Request.Deadline)
	defer cancel()
	payload, _ := json.Marshal(command.Candidate)
	commandDigest, err := candidateRequestDigest(command.Scope, command.DocumentID, command.Candidate)
	if err != nil {
		return receipt, ErrRepository
	}
	verifyWrite := func(c context.Context, scope contracts.AccessScope) error {
		if p.Verify == nil || p.Verify(c, cloneScope(scope)) != nil {
			return ErrRepository
		}
		var owned document.CandidateCommand
		if json.Unmarshal(raw, &owned) != nil || p.AuthorizeCandidate(c, owned) != nil || c.Err() != nil {
			return ErrRepository
		}
		return nil
	}
	err = withinSession(ctx, p.DB, command.Scope, verifyWrite, func(c context.Context, s *session) error {
		if err := s.enter(c, command.Scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		args := []any{string(command.Scope.SpaceID), string(command.Scope.KnowledgeBaseIDs[0]), string(command.DocumentID)}
		var alive bool
		if s.tx.QueryRowContext(c, `SELECT NOT deleted FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR SHARE`, args...).Scan(&alive) != nil || !alive {
			return ErrRepository
		}
		var existingDigest string
		var existing []byte
		err := s.tx.QueryRowContext(c, `SELECT request_digest,receipt FROM akos_withdraw_v1.draft_candidates WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text`, args[0], args[1], string(command.CandidateID)).Scan(&existingDigest, &existing)
		if err == nil {
			if existingDigest != commandDigest || json.Unmarshal(existing, &receipt) != nil || receipt.CandidateID != command.CandidateID || receipt.DraftID != command.Candidate.DraftID || receipt.DocumentID != command.DocumentID || receipt.DraftVersion == 0 {
				return ErrRepository
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return ErrRepository
		}
		var stored []byte
		var version string
		if s.tx.QueryRowContext(c, `SELECT payload,row_version::text FROM akos_withdraw_v1.draft_heads WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND id=$4::text FOR UPDATE`, append(args, string(command.Candidate.DraftID))...).Scan(&stored, &version) != nil {
			return ErrRepository
		}
		var draft contracts.Draft
		if json.Unmarshal(stored, &draft) != nil || draft.ID != command.Candidate.DraftID || draft.DocumentID != command.DocumentID || strconv.FormatUint(draft.RowVersion, 10) != version {
			return ErrRepository
		}
		merged, err := document.MergeParsedCandidate(draft, command.Candidate)
		if err != nil {
			return ErrRepository
		}
		if merged.Applied {
			updated, err := json.Marshal(merged.Draft)
			if err != nil {
				return ErrRepository
			}
			writeArgs := append(args, string(draft.ID), version, strconv.FormatUint(merged.Draft.RowVersion, 10), string(updated))
			result, err := s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.draft_heads SET row_version=$6::numeric,payload=$7::jsonb WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND id=$4::text AND row_version=$5::numeric AND state='draft'`, writeArgs...)
			if err != nil {
				return ErrRepository
			}
			if count, err := result.RowsAffected(); err != nil || count != 1 {
				return ErrRepository
			}
		}
		receipt = document.CandidateReceipt{CandidateID: command.CandidateID, DraftID: draft.ID, DocumentID: command.DocumentID, Applied: merged.Applied, Reason: merged.Reason, DraftVersion: merged.Draft.RowVersion}
		receiptBytes, err := json.Marshal(receipt)
		if err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.draft_candidates(space_id,kb_id,document_id,draft_id,id,actor_id,request_digest,payload,receipt) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::jsonb,$9::jsonb)`, append(args, string(draft.ID), string(command.CandidateID), string(command.Scope.SubjectID), commandDigest, string(payload), string(receiptBytes))...); err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.candidate_audit(space_id,kb_id,candidate_id,actor_id,request_id,action) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,'document.parse')`, args[0], args[1], string(command.CandidateID), string(command.Scope.SubjectID), string(command.Request.RequestID)); err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.candidate_outbox VALUES($1::text,$2::text,$3::text,'draft.candidate.recorded',$4::jsonb)`, args[0], args[1], string(command.CandidateID), string(receiptBytes)); err != nil {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return document.CandidateReceipt{}, err
	}
	return receipt, nil
}

func candidateRequestDigest(scope contracts.AccessScope, documentID contracts.ID, candidate document.ParseCandidate) (string, error) {
	payload, err := json.Marshal(candidate)
	if err != nil {
		return "", ErrRepository
	}
	bound, err := json.Marshal(struct {
		Space, Actor contracts.ID
		KBs          []contracts.ID
		Document     contracts.ID
		Candidate    json.RawMessage
	}{scope.SpaceID, scope.SubjectID, scope.KnowledgeBaseIDs, documentID, payload})
	if err != nil {
		return "", ErrRepository
	}
	digest := sha256.Sum256(append([]byte("akos.draft-candidate/1\n"), bound...))
	return hex.EncodeToString(digest[:]), nil
}
