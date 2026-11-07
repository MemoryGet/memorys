package contracts

import (
	"context"
	"time"
)

type FeedbackListInput struct {
	Credential      Credential
	Request         RequestContext
	KnowledgeBaseID ID
	PageSize        int
	Cursor          string
}

type FeedbackItem struct {
	ID              ID
	SpaceID         ID
	KnowledgeBaseID ID
	DocumentID      ID
	RevisionID      ID
	BlockID         ID
	TargetRequestID ID
	Kind            string
	Comment         string
	State           string
	CreatedAt       time.Time
}

type FeedbackPage struct {
	Items      []FeedbackItem
	NextCursor string
}

type FeedbackListApplication interface {
	ProfileIdentity() (uint64, Digest, error)
	ListFeedback(context.Context, FeedbackListInput) (FeedbackPage, error)
}
