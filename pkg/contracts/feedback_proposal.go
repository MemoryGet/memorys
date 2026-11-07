package contracts

import (
	"context"
	"errors"
	"time"
)

var ErrFeedbackProposalConflict = errors.New("feedback proposal idempotency conflict")

// FeedbackProposalInput turns an acknowledged feedback record into a new
// editable draft. The source revision is copied; the proposal never mutates or
// publishes that revision.
type FeedbackProposalInput struct {
	Credential     Credential
	Request        RequestContext
	FeedbackID     ID
	Comment        string
	IdempotencyKey string
}

type FeedbackProposal struct {
	ID         ID
	FeedbackID ID
	DraftID    ID
	State      string
	CreatedAt  time.Time
}

type FeedbackProposalApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ProposeFeedback(context.Context, FeedbackProposalInput) (FeedbackProposal, error)
}
