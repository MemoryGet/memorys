package contracts

import (
	"context"
	"errors"
)

var ErrDraftEditConflict = errors.New("draft edit conflicts with current version")

type DraftEditInput struct {
	Credential      Credential
	Request         RequestContext
	DraftID         ID
	ExpectedVersion uint64
	Blocks          []DraftBlock
	IdempotencyKey  string
}

type DraftEditApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	EditDraft(context.Context, DraftEditInput) (Draft, error)
}
