package document

import (
	"context"
	"errors"
	"math"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrReviewRejected = errors.New("review rejected without effects from this invocation")
var ErrReviewUnknown = errors.New("review commit requires reconciliation with the same key")

type ReviewCommand struct {
	DocumentID contracts.ID
	Request    contracts.ApprovalDecisionRequest
}

// ReviewRepository atomically appends a decision, advances the review request,
// and records the receipt, audit and outbox. Replay requires current authority.
type ReviewRepository interface {
	DecideReview(context.Context, ReviewCommand) (contracts.Approval, error)
}

type ReviewPreparation struct {
	Request  contracts.ApprovalRequest
	Decision contracts.Approval
}

// PrepareReview binds the decision to immutable content and a single request
// version. It does not authenticate the reviewer or prove current validity.
func PrepareReview(request contracts.ApprovalRequest, fact contracts.FrozenRevisionFact, expected uint64, decisionID, actor contracts.ID, decision, reason string, policy uint64, at time.Time) (ReviewPreparation, error) {
	if request.RowVersion != expected || expected == math.MaxUint64 || !frozenID(request.ID) || !frozenID(request.DocumentID) || !frozenID(request.RevisionID) || !frozenID(decisionID) || !frozenID(actor) || policy == 0 || at.IsZero() || at.Before(request.SubmittedAt) || at.UTC().Year() < 1 || at.UTC().Year() > 9999 || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > 10000 || fact.ID != request.RevisionID || fact.DocumentID != request.DocumentID || fact.HashFormat != "akos.freeze/1" || !candidateDigest(fact.ContentHash) || !candidateDigest(fact.BlocksHash) || request.ContentHash != fact.ContentHash || request.PolicyVersion == 0 || request.SubmittedAt.IsZero() || !frozenID(request.SubmittedBy) {
		return ReviewPreparation{}, ErrReviewRejected
	}
	switch decision {
	case "approve", "reject":
		if request.State != "submitted" || request.RowVersion != 0 {
			return ReviewPreparation{}, ErrReviewRejected
		}
	case "revoke":
		if request.State != "approved" || request.RowVersion == 0 {
			return ReviewPreparation{}, ErrReviewRejected
		}
	default:
		return ReviewPreparation{}, ErrReviewRejected
	}
	request.RowVersion++
	request.PolicyVersion = policy
	switch decision {
	case "approve":
		request.State = "approved"
	case "reject":
		request.State = "rejected"
	case "revoke":
		request.State = "revoked"
	}
	approval := contracts.Approval{ID: decisionID, RevisionID: fact.ID, ContentHash: fact.ContentHash, PolicyVersion: policy, Decision: decision, Reason: reason, ActorID: actor, RowVersion: request.RowVersion, DecidedAt: at.UTC()}
	return ReviewPreparation{Request: request, Decision: approval}, nil
}
