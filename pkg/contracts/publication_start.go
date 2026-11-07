package contracts

import "context"

type PublicationStartInput struct {
	Credential     Credential
	Request        RequestContext
	DocumentID     ID
	RevisionID     ID
	ApprovalID     ID
	ExpectedEpoch  uint64
	IdempotencyKey string
}

type PublicationStartApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	StartPublication(context.Context, PublicationStartInput) (PublicationStartResult, error)
}
