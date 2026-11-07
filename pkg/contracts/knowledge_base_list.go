package contracts

import "context"

type KnowledgeBase struct {
	ID, SpaceID               ID
	Name                      string
	ActiveBackendEpochID      *ID
	RowVersion, PolicyVersion uint64
}

type KnowledgeBaseListInput struct {
	Credential Credential
	Request    RequestContext
	SpaceID    ID
	PageSize   int
	Cursor     string
}
type KnowledgeBasePage struct {
	Items      []KnowledgeBase
	NextCursor string
}

type KnowledgeBaseListApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ListKnowledgeBases(context.Context, KnowledgeBaseListInput) (KnowledgeBasePage, error)
}
