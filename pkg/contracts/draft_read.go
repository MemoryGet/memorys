package contracts

import "context"

// DraftReadInput carries the authenticated management read request. The
// transport layer is responsible for validating the path and bearer token;
// the application must still authenticate and authorize again before reading.
type DraftReadInput struct {
	Credential Credential
	Request    RequestContext
	DraftID    ID
}

type DraftReadApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ReadDraft(context.Context, DraftReadInput) (Draft, error)
}
