package contracts

import "context"

// ReviewDecisionInput is the authenticated transport-independent review
// command consumed by management adapters and the PostgreSQL review UoW.
type ReviewDecisionInput struct {
	Credential              Credential
	Request                 RequestContext
	RevisionID              ID
	Decision                string
	Reason                  string
	ExpectedApprovalVersion uint64
	IdempotencyKey          string
}

type ReviewDecisionApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	DecideReview(context.Context, ReviewDecisionInput) (Approval, error)
}
