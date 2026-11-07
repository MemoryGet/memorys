package contracts

import "context"

type DraftSubmitInput struct {
	Credential      Credential
	Request         RequestContext
	DraftID         ID
	ExpectedVersion uint64
	IdempotencyKey  string
}

type DraftSubmitApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	SubmitDraft(context.Context, DraftSubmitInput) (SubmitResult, error)
}
