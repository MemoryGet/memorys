package contracts

import "context"

type RevisionReadInput struct {
	Credential Credential
	Request    RequestContext
	RevisionID ID
}

type RevisionReadApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ReadRevision(context.Context, RevisionReadInput) (Revision, error)
}
