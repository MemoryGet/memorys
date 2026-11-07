package contracts

import "context"

type PublicationOperationReadInput struct {
	Credential  Credential
	Request     RequestContext
	OperationID ID
}

type PublicationOperationReadApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ReadPublicationOperation(context.Context, PublicationOperationReadInput) (PublicationOperation, error)
}
