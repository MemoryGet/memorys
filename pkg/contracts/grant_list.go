package contracts

import "context"

type GrantListInput struct {
	Credential      Credential
	Request         RequestContext
	KnowledgeBaseID ID
	PageSize        int
	Cursor          string
}

type GrantItem struct {
	KnowledgeBaseID ID
	SubjectID       ID
	Role            string
	PolicyVersion   uint64
}

type GrantPage struct {
	Items      []GrantItem
	NextCursor string
}

type GrantListApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ListGrants(context.Context, GrantListInput) (GrantPage, error)
}
