package contracts

import "context"

type DeletionCleanupTicket struct {
	JobID, ResourceID, SpaceID, KnowledgeBaseID ID
	ProfileDigest                               Digest
}

// Internal keyset cursor, never an authorization grant or public HTTP token.
type DeletionQueueCursor struct {
	AfterJobID, UpperJobID ID
	Binding                Digest
}
type DeletionQueueRequest struct {
	Request  RequestContext
	Scope    AccessScope
	WorkerID ID
	Limit    int
	Cursor   *DeletionQueueCursor
}
type DeletionQueuePage struct {
	Items      []DeletionCleanupTicket
	UpperJobID ID
	Next       *DeletionQueueCursor
}
type DeletionCleanupQueue interface {
	DiscoverDeletionCleanup(context.Context, DeletionQueueRequest) (DeletionQueuePage, error)
}
type DeletionJobRunner interface {
	RunOne(context.Context, DeletionCleanupRequest) (ResourceJob, error)
}
