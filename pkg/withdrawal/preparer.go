package withdrawal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

const ActorDigestFormat = "akos.withdraw.actor/1"
const CommandDigestFormat = "akos.withdraw.command/1"

type Semantics struct {
	Actor, Command             []byte
	ActorDigest, CommandDigest contracts.Digest
}

func validID(s string) bool {
	return utf8.ValidString(s) && len(s) > 0 && utf8.RuneCountInString(s) <= 200
}
func validDigest(s contracts.Digest) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func encodeSemantic(v any, format string) ([]byte, contracts.Digest, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, "", ErrRejected
	}
	data, err := canonical.JSON(raw)
	if err != nil {
		return nil, "", ErrRejected
	}
	h := sha256.New()
	h.Write([]byte(format + "\n"))
	h.Write(data)
	return data, contracts.Digest(hex.EncodeToString(h.Sum(nil))), nil
}

// SemanticCommitments validates stable field shapes. It authenticates nothing.
// No request/scope IDs, deadlines, token proof references or policy versions
// enter the commitments; a retry must still verify them against current authority.
func SemanticCommitments(request contracts.WithdrawRequest) (Semantics, error) {
	c, p := request.Command, request.Command.Principal
	if c.Scope.Action != "publication.withdraw" || c.Scope.SubjectID != p.SubjectID || len(c.Scope.KnowledgeBaseIDs) != 1 || !validID(string(c.Scope.SpaceID)) || !validID(string(c.Scope.KnowledgeBaseIDs[0])) || !validID(string(p.SubjectID)) || p.SubjectType != "user" && p.SubjectType != "service" || p.Issuer == "" || !utf8.ValidString(p.Issuer) || utf8.RuneCountInString(p.Issuer) > 4096 || p.ClientID != "" && !validID(string(p.ClientID)) || !validID(string(request.DocumentID)) || !validID(c.IdempotencyKey) || request.ExpectedEpoch == 0 || request.ExpectedEpoch == math.MaxUint64 || !validDigest(c.Request.ProfileDigest) || request.Reason == "" || !utf8.ValidString(request.Reason) || utf8.RuneCountInString(request.Reason) > 10000 {
		return Semantics{}, ErrRejected
	}
	var delegation any
	if p.Delegation != nil {
		d := p.Delegation
		if !validID(string(d.UserID)) || !validID(string(d.ClientID)) || p.ClientID != d.ClientID || d.ProofReference == "" || !utf8.ValidString(d.ProofReference) || len(d.ProofReference) > 4096 {
			return Semantics{}, ErrRejected
		}
		delegation = map[string]any{"user_id": d.UserID, "client_id": d.ClientID}
	}
	a, ad, err := encodeSemantic(map[string]any{"format": ActorDigestFormat, "subject_id": p.SubjectID, "subject_type": p.SubjectType, "issuer": p.Issuer, "client_id": p.ClientID, "delegation": delegation}, ActorDigestFormat)
	if err != nil {
		return Semantics{}, err
	}
	b, bd, err := encodeSemantic(map[string]any{"format": CommandDigestFormat, "action": "publication.withdraw", "actor_digest": ad, "space_id": c.Scope.SpaceID, "knowledge_base_id": c.Scope.KnowledgeBaseIDs[0], "document_id": request.DocumentID, "expected_epoch": strconv.FormatUint(request.ExpectedEpoch, 10), "reason": request.Reason, "profile_digest": c.Request.ProfileDigest}, CommandDigestFormat)
	if err != nil {
		return Semantics{}, err
	}
	return Semantics{a, b, ad, bd}, nil
}

type Preparer struct {
	// Required: verify authenticated principal/delegation and current scoped
	// document authority. A client supplied Principal/Scope is not sufficient.
	Verify func(context.Context, contracts.WithdrawRequest) error
	NewID  func(context.Context) (contracts.ID, error)
	Now    func() time.Time
}

func randomID(ctx context.Context) (contracts.ID, error) {
	if ctx.Err() != nil {
		return "", ErrRejected
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", ErrRejected
	}
	return contracts.ID("wd-" + hex.EncodeToString(bytes[:])), nil
}
func (p Preparer) Prepare(ctx context.Context, request contracts.WithdrawRequest) (Preparation, error) {
	if ctx == nil || p.Verify == nil {
		return Preparation{}, ErrRejected
	}
	if _, ok := ctx.Deadline(); !ok {
		return Preparation{}, ErrRejected
	}
	request = cloneRequest(request)
	if !validID(string(request.Command.Request.RequestID)) || !validID(string(request.Command.Scope.ScopeID)) || request.Command.Scope.PolicyVersion == 0 || request.Command.Request.Deadline.IsZero() || request.Command.Scope.ExpiresAt.IsZero() {
		return Preparation{}, ErrRejected
	}
	deadline := request.Command.Request.Deadline
	if request.Command.Scope.ExpiresAt.Before(deadline) {
		deadline = request.Command.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if ctx.Err() != nil {
		return Preparation{}, ErrRejected
	}
	semantic, err := SemanticCommitments(request)
	if err != nil {
		return Preparation{}, err
	}
	if err := p.Verify(ctx, cloneRequest(request)); err != nil {
		return Preparation{}, err
	}
	if ctx.Err() != nil {
		return Preparation{}, ErrRejected
	}
	newID := p.NewID
	if newID == nil {
		newID = randomID
	}
	ids := make([]contracts.ID, 5)
	seen := map[contracts.ID]bool{}
	for i := range ids {
		if ctx.Err() != nil {
			return Preparation{}, ErrRejected
		}
		id, err := newID(ctx)
		if err != nil {
			return Preparation{}, err
		}
		if !validID(string(id)) || seen[id] {
			return Preparation{}, ErrRejected
		}
		ids[i] = id
		seen[id] = true
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	occurred := now().UTC()
	if ctx.Err() != nil || occurred.IsZero() || occurred.Year() < 1 || occurred.Year() > 9999 {
		return Preparation{}, ErrRejected
	}
	return Preparation{OperationID: ids[0], HistoryID: ids[1], PlanID: ids[2], EventID: ids[3], AuditID: ids[4], ActorSemanticDigest: semantic.ActorDigest, CommandDigest: semantic.CommandDigest, OccurredAt: occurred}, nil
}
