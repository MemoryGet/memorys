package document

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrDeletion = errors.New("document deletion rejected or unavailable")

func ownDeletion(r contracts.DeleteDocumentRequest) (contracts.DeleteDocumentRequest, error) {
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > 65536 {
		return contracts.DeleteDocumentRequest{}, ErrDeletion
	}
	var owned contracts.DeleteDocumentRequest
	if json.Unmarshal(raw, &owned) != nil {
		return owned, ErrDeletion
	}
	return owned, nil
}

// DeletionCommitments bind stable intent and actor semantics, never transient
// proof references, scope IDs, deadlines or policy versions. Authentication is
// still mandatory on every call, including committed-result recovery.
func DeletionCommitments(r contracts.DeleteDocumentRequest) (contracts.Digest, contracts.Digest, error) {
	c, p := r.Command, r.Command.Principal
	if c.Scope.Action != "document.delete" || c.Scope.SubjectID != p.SubjectID || len(c.Scope.KnowledgeBaseIDs) != 1 || !frozenID(c.Scope.SpaceID) || !frozenID(c.Scope.KnowledgeBaseIDs[0]) || !frozenID(r.DocumentID) || !frozenID(p.SubjectID) || !frozenID(contracts.ID(c.IdempotencyKey)) || p.SubjectType != "user" && p.SubjectType != "service" || p.Issuer == "" || !utf8.ValidString(p.Issuer) || len(p.Issuer) > 4096 || p.ClientID != "" && !frozenID(p.ClientID) || r.ExpectedDocumentVersion == 0 || r.ExpectedDocumentVersion == math.MaxUint64 || !candidateDigest(c.Request.ProfileDigest) {
		return "", "", ErrDeletion
	}
	var delegation any
	if d := p.Delegation; d != nil {
		if !frozenID(d.UserID) || !frozenID(d.ClientID) || p.ClientID != d.ClientID || d.ProofReference == "" || !utf8.ValidString(d.ProofReference) || len(d.ProofReference) > 4096 {
			return "", "", ErrDeletion
		}
		delegation = map[string]any{"user_id": d.UserID, "client_id": d.ClientID}
	}
	digest := func(format string, value any) (contracts.Digest, error) {
		raw, e := json.Marshal(value)
		if e != nil {
			return "", ErrDeletion
		}
		raw, e = canonical.JSON(raw)
		if e != nil {
			return "", ErrDeletion
		}
		sum := sha256.Sum256(append([]byte(format+"\n"), raw...))
		return contracts.Digest(hex.EncodeToString(sum[:])), nil
	}
	actor, e := digest("akos.delete.actor/1", map[string]any{"subject_id": p.SubjectID, "subject_type": p.SubjectType, "issuer": p.Issuer, "client_id": p.ClientID, "delegation": delegation})
	if e != nil {
		return "", "", e
	}
	command, e := digest("akos.delete.command/1", map[string]any{"action": "document.delete", "actor_digest": actor, "space_id": c.Scope.SpaceID, "kb_id": c.Scope.KnowledgeBaseIDs[0], "document_id": r.DocumentID, "expected_document_version": strconv.FormatUint(r.ExpectedDocumentVersion, 10), "profile_digest": c.Request.ProfileDigest})
	return actor, command, e
}

type DeletionService struct {
	Store contracts.DocumentDeletionStore
	// Verify must authenticate identity/delegation and authorize the actual
	// document. A client-supplied Principal/Scope is never sufficient.
	Verify func(context.Context, contracts.DeleteDocumentRequest) error
}

func (s DeletionService) Delete(ctx context.Context, input contracts.DeleteDocumentRequest) (contracts.DeleteDocumentResult, error) {
	var empty contracts.DeleteDocumentResult
	r, e := ownDeletion(input)
	if e != nil || ctx == nil || s.Store == nil || s.Verify == nil {
		return empty, ErrDeletion
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrDeletion
	}
	c := r.Command
	if !frozenID(c.Request.RequestID) || !frozenID(c.Scope.ScopeID) || c.Scope.PolicyVersion == 0 || c.Request.Deadline.IsZero() || c.Scope.ExpiresAt.IsZero() {
		return empty, ErrDeletion
	}
	ctx, cancel := context.WithDeadline(ctx, c.Request.Deadline)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, c.Scope.ExpiresAt)
	defer expire()
	actor, command, e := DeletionCommitments(r)
	if e != nil || ctx.Err() != nil {
		return empty, ErrDeletion
	}
	check, _ := ownDeletion(r)
	if e = s.Verify(ctx, check); e != nil {
		return empty, e
	}
	if ctx.Err() != nil {
		return empty, ErrDeletion
	}
	ids := make([]contracts.ID, 6)
	for i := range ids {
		var b [16]byte
		if _, e = rand.Read(b[:]); e != nil {
			return empty, ErrDeletion
		}
		ids[i] = contracts.ID("del-" + hex.EncodeToString(b[:]))
	}
	owned, _ := ownDeletion(r)
	result, e := s.Store.DeleteDocument(ctx, contracts.DocumentDeletionCommand{Request: owned, OperationID: ids[0], HistoryID: ids[1], PlanID: ids[2], EventID: ids[3], AuditID: ids[4], JobID: ids[5], ActorDigest: actor, CommandDigest: command, OccurredAt: time.Now().UTC()})
	if e != nil {
		return empty, e
	}
	check, _ = ownDeletion(r)
	if e = s.Verify(ctx, check); e != nil {
		return empty, e
	}
	if ctx.Err() != nil {
		return empty, ErrDeletion
	}
	if result.DocumentID != r.DocumentID || result.DocumentVersion != r.ExpectedDocumentVersion+1 || result.PublicationEpoch == 0 || result.JobID == "" || result.OperationID == "" || result.HistoryID == "" || result.DeletedAt.IsZero() {
		return empty, ErrDeletion
	}
	return result, nil
}
