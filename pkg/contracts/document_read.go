package contracts

import "context"

type DocumentReadInput struct {
	Credential Credential
	Request    RequestContext
	DocumentID ID
}

type DocumentReadApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ReadDocument(context.Context, DocumentReadInput) (Document, error)
}
