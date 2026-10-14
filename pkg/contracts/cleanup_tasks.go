package contracts

import (
	"context"
	"time"
)

// Draft cleanup-specific ports, not the complete TaskRepository/TaskQueue.
type CleanupLease struct {
	SpaceID, KnowledgeBaseID, DocumentID, PlanID, TargetID, WorkerID ID
	RevisionID, GenerationSetID                                      ID
	PublicationEpoch, FencingToken, Attempt, RowVersion              uint64
	ExpiresAt                                                        time.Time
	Target                                                           CleanupTarget
}
type CleanupTaskStore interface {
	Claim(context.Context, AccessScope, ID, ID, ID, time.Duration) (CleanupLease, error)
	Finish(context.Context, AccessScope, CleanupLease, string, ID) error
}
