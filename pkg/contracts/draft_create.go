package contracts

import "context"

type DraftCreateInput struct {
	Credential              Credential
	Request                 RequestContext
	DocumentID              ID
	BaseRevisionID          *ID
	ExpectedDocumentVersion uint64
	IdempotencyKey          string
}

type DraftCreateApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	CreateDraft(context.Context, DraftCreateInput) (Draft, error)
}
