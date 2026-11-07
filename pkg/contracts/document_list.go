package contracts

import "context"

type DocumentListInput struct {
	Credential      Credential
	Request         RequestContext
	KnowledgeBaseID ID
	PageSize        int
	Cursor          string
}

type DocumentPage struct {
	Items      []Document
	NextCursor string
}

type DocumentListApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ListDocuments(context.Context, DocumentListInput) (DocumentPage, error)
}
