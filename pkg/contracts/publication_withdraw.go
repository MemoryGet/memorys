package contracts

import "context"

type PublicationWithdrawInput struct {
	Credential     Credential
	Request        RequestContext
	DocumentID     ID
	ExpectedEpoch  uint64
	Reason         string
	IdempotencyKey string
}

type PublicationWithdrawApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	WithdrawPublication(context.Context, PublicationWithdrawInput) (WithdrawResult, error)
}
