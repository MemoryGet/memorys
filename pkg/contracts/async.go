package contracts

import (
	"context"
	"time"
)

type Job struct {
	ID            ID
	SpaceID       ID
	OperationID   ID
	RevisionID    *ID
	Stage         string
	ProfileDigest Digest
	GenerationID  *ID
	State         string
	Attempt       uint64
	FencingToken  uint64
	LeaseUntil    *time.Time
	Progress      float64
	RowVersion    uint64
	Error         *PublicError
}

type Lease struct {
	JobID        ID
	WorkerID     ID
	AttemptID    ID
	FencingToken uint64
	ExpiresAt    time.Time
}

type ClaimRequest struct {
	Request       RequestContext
	Scope         AccessScope
	JobID         ID
	WorkerID      ID
	LeaseDuration time.Duration
}

type TaskCompleteRequest struct {
	Request            RequestContext
	Scope              AccessScope
	Lease              Lease
	ResultReference    ID
	ExpectedJobVersion uint64
	Outcome            string
	Error              *PublicError
}

type TaskStateCommand struct {
	Command            CommandContext
	JobID              ID
	ExpectedJobVersion uint64
}

type TaskRepository interface {
	Create(context.Context, AccessScope, Job) error
	Load(context.Context, AccessScope, ID) (Job, error)
	Claim(context.Context, ClaimRequest) (Lease, error)
	Renew(context.Context, AccessScope, Lease, time.Duration) (Lease, error)
	Complete(context.Context, TaskCompleteRequest) error
	Cancel(context.Context, TaskStateCommand) (Job, error)
	Retry(context.Context, TaskStateCommand) (Job, error)
}

type DeliveryReceipt struct {
	JobID     ID
	ReceiptID string // Opaque transport receipt, never a task ownership token.
}

type ReceiveRequest struct {
	Request  RequestContext
	WorkerID ID
	Limit    int
}

type TaskQueue interface {
	Enqueue(context.Context, ID) error
	Receive(context.Context, ReceiveRequest) ([]DeliveryReceipt, error)
	Ack(context.Context, DeliveryReceipt) error
	Retry(context.Context, DeliveryReceipt, time.Duration) error
}

type EventPayload struct {
	ResourceIDs   []ID
	RevisionID    *ID
	State         string
	ContentDigest Digest
}

type EventEnvelope struct {
	ID               ID
	Type             string
	SchemaVersion    uint64
	AggregateID      ID
	AggregateVersion uint64
	OperationID      ID
	SpaceID          ID
	OccurredAt       time.Time
	ProfileDigest    Digest
	Payload          EventPayload
}

type EventConsumeRequest struct {
	Request    RequestContext
	ConsumerID ID
	Cursor     string
	Limit      int
}

type EventPage struct {
	Events     []EventEnvelope
	NextCursor string
}

type EventBus interface {
	Publish(context.Context, EventEnvelope) error
	Consume(context.Context, EventConsumeRequest) (EventPage, error)
}
