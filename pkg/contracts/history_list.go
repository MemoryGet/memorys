package contracts

import "context"

type PublicationHistoryListInput struct {
	Credential Credential
	Request    RequestContext
	DocumentID ID
	PageSize   int
	Cursor     string
}

type PublicationHistoryPage struct {
	Items      []PublicationHistory
	NextCursor string
}

type PublicationHistoryListApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ListPublicationHistory(context.Context, PublicationHistoryListInput) (PublicationHistoryPage, error)
}
