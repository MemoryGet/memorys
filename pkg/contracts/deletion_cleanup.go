package contracts

import (
	"context"
	"time"
)

const (
	DeletionCleanupCancelledEvent = "document.delete.cleanup.cancelled"
	DeletionCleanupRetryEvent     = "document.delete.cleanup.retry_requested"
)

type DeletionCleanupRequest struct {
	Request             RequestContext
	Scope               AccessScope
	JobID, WorkerID     ID
	TargetLeaseDuration time.Duration
}

type DeletionCleanupSelection struct {
	Record           ResourceJob
	PlanID, TargetID ID
	HasTarget        bool
}

type DeletionCleanupStore interface {
	NextDeletionCleanup(context.Context, DeletionCleanupRequest) (DeletionCleanupSelection, error)
	ReconcileDeletionCleanup(context.Context, DeletionCleanupRequest) (ResourceJob, error)
}

type DeletionCleanupControlRequest struct {
	Request            RequestContext
	Scope              AccessScope
	JobID              ID
	ExpectedJobVersion uint64
}

// Cancellation is a durable terminal decision. Retry is a separate explicit
// control operation and cannot reset unknown effects or take over an active
// target lease.
type DeletionCleanupControl interface {
	CancelDeletionCleanup(context.Context, DeletionCleanupControlRequest) (ResourceJob, error)
	// RetryDeletionCleanup is an explicit operator decision. Implementations
	// must reject unknown effects and active leases; callers must never infer a
	// retry from a transient transport error.
	RetryDeletionCleanup(context.Context, DeletionCleanupControlRequest) (ResourceJob, error)
}

type CleanupTargetRunner interface {
	RunOne(context.Context, AccessScope, ID, ID, ID, time.Duration) error
}
