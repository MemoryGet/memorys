package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// CandidateReader is a separate read capability. It cannot write candidates or
// infer a read grant from the parser's original-read or candidate-write grant.
type CandidateReader struct {
	DB            *sql.DB
	Verify        VerifyScope
	AuthorizeRead func(context.Context, document.CandidateReadRequest) error
}

var _ document.CandidateReader = CandidateReader{}

func (p CandidateReader) ReadCandidate(ctx context.Context, request document.CandidateReadRequest) (document.CandidateRecord, error) {
	var record document.CandidateRecord
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > 1<<20 || p.AuthorizeRead == nil || ctx == nil {
		return record, document.ErrCandidateRead
	}
	var frozen document.CandidateReadRequest
	if json.Unmarshal(raw, &frozen) != nil {
		return record, document.ErrCandidateRead
	}
	request = frozen
	if _, ok := ctx.Deadline(); !ok {
		return record, document.ErrCandidateRead
	}
	if request.Scope.Action != "document.candidate.read" || request.DocumentID == "" || request.DraftID == "" || request.CandidateID == "" || request.Request.RequestID == "" || request.Request.Deadline.IsZero() {
		return record, document.ErrCandidateRead
	}
	ctx, cancel := context.WithDeadline(ctx, request.Request.Deadline)
	defer cancel()
	verify := func(c context.Context, scope contracts.AccessScope) error {
		if p.Verify == nil || p.Verify(c, cloneScope(scope)) != nil {
			return ErrRepository
		}
		var owned document.CandidateReadRequest
		if json.Unmarshal(raw, &owned) != nil || p.AuthorizeRead(c, owned) != nil || c.Err() != nil {
			return ErrRepository
		}
		return nil
	}
	err = withinSession(ctx, p.DB, request.Scope, verify, func(c context.Context, s *session) error {
		if err := s.enter(c, request.Scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		var payload, receipt []byte
		var storedDigest string
		if s.tx.QueryRowContext(c, `SELECT c.payload,c.receipt,c.actor_id,c.request_digest
 FROM akos_withdraw_v1.draft_candidates c JOIN akos_withdraw_v1.documents d
 ON d.space_id=c.space_id AND d.kb_id=c.kb_id AND d.id=c.document_id
 WHERE c.space_id=$1::text AND c.kb_id=$2::text AND c.document_id=$3::text AND c.draft_id=$4::text AND c.id=$5::text AND NOT d.deleted FOR SHARE OF d`, string(request.Scope.SpaceID), string(request.Scope.KnowledgeBaseIDs[0]), string(request.DocumentID), string(request.DraftID), string(request.CandidateID)).Scan(&payload, &receipt, &record.ActorID, &storedDigest) != nil {
			return ErrRepository
		}
		if len(payload) > 8<<20 || len(receipt) > 65536 || record.ActorID == "" || json.Unmarshal(payload, &record.Candidate) != nil || json.Unmarshal(receipt, &record.Receipt) != nil {
			return ErrRepository
		}
		if record.Candidate.DraftID != request.DraftID || record.Receipt.CandidateID != request.CandidateID || record.Receipt.DraftID != request.DraftID || record.Receipt.DocumentID != request.DocumentID || record.Receipt.DraftVersion == 0 {
			return ErrRepository
		}
		candidate := record.Candidate
		validation := contracts.Draft{ID: request.DraftID, DocumentID: request.DocumentID, State: "draft", RowVersion: candidate.ExpectedVersion, Blobs: []contracts.BlobRef{candidate.Input}}
		if _, err := document.MergeParsedCandidate(validation, candidate); err != nil {
			return ErrRepository
		}
		// Reconstruct the original actor binding, not the current reviewer identity.
		bound := cloneScope(request.Scope)
		bound.SubjectID = record.ActorID
		digest, err := candidateRequestDigest(bound, request.DocumentID, candidate)
		if err != nil || digest != storedDigest {
			return ErrRepository
		}
		if record.Receipt.Applied {
			if record.Receipt.DraftVersion != candidate.ExpectedVersion+1 || record.Receipt.Reason != "" {
				return ErrRepository
			}
		} else {
			switch record.Receipt.Reason {
			case "draft_changed", "draft_not_editable", "original_changed", "multiple_originals":
			default:
				return ErrRepository
			}
		}
		return nil
	})
	if err != nil {
		return document.CandidateRecord{}, document.ErrCandidateRead
	}
	return record, nil
}
