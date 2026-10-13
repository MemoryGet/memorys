package contracts

import (
	"context"
	"time"
)

type Document struct {
	ID                    ID
	SpaceID               ID
	KnowledgeBaseID       ID
	Title                 string
	CreatedBy             ID
	ActiveRevisionID      *ID
	ActiveGenerationSetID *ID
	PublicationEpoch      uint64
	RowVersion            uint64
	DeletedAt             *time.Time
}

type Draft struct {
	ID             ID
	DocumentID     ID
	BaseRevisionID *ID
	Blocks         []DraftBlock
	Blobs          []BlobRef
	RowVersion     uint64
	State          string
}

type Revision struct {
	ID               ID
	DocumentID       ID
	ParentRevisionID *ID
	Blocks           []Block
	Blobs            []BlobRef
	ContentHash      Digest
	BlocksHash       Digest
	HashFormat       string // Immutable format identifier, e.g. akos.freeze/1.
	CreatedBy        ID
	CreatedAt        time.Time
}

type CommandContext struct {
	Request        RequestContext
	Principal      Principal
	Scope          AccessScope
	IdempotencyKey string
}

type SourceDescriptor struct {
	Kind            string
	Blob            *BlobRef
	ConnectorID     ID
	ResourceLocator string
}

type IngestRequest struct {
	Command         CommandContext
	KnowledgeBaseID ID
	Title           string
	Source          SourceDescriptor
}

type IngestResult struct {
	Document Document
	Draft    Draft
	Job      Job
}

type SubmitRequest struct {
	Command         CommandContext
	DraftID         ID
	ExpectedVersion uint64
}

type SubmitResult struct {
	Revision          Revision
	ApprovalRequestID ID
}

type Approval struct {
	ID            ID
	RevisionID    ID
	ContentHash   Digest
	PolicyVersion uint64
	Decision      string
	Reason        string
	ActorID       ID
	RowVersion    uint64
	DecidedAt     time.Time
}

type ApprovalDecisionRequest struct {
	Command                 CommandContext
	RevisionID              ID
	Decision                string
	Reason                  string
	ExpectedApprovalVersion uint64
}

type ApprovalRequest struct {
	ID            ID
	RevisionID    ID
	DocumentID    ID
	SubmittedBy   ID
	SubmittedAt   time.Time
	State         string
	RowVersion    uint64
	ContentHash   Digest
	PolicyVersion uint64
}

type PublishRequest struct {
	Command       CommandContext
	DocumentID    ID
	RevisionID    ID
	ApprovalID    ID
	ExpectedEpoch uint64
}

type PublicationOperation struct {
	ID              ID
	DocumentID      ID
	RevisionID      ID
	ApprovalID      ID
	GenerationSetID *ID
	ExpectedEpoch   uint64
	State           string
	ActorID         ID
}

type WithdrawRequest struct {
	Command       CommandContext
	DocumentID    ID
	ExpectedEpoch uint64
	Reason        string
}

type WithdrawResult struct {
	DocumentID       ID
	PublicationEpoch uint64
	HistoryID        ID
}

type Ingestor interface {
	Ingest(context.Context, IngestRequest) (IngestResult, error)
}

type FeedbackRequest struct {
	Command         CommandContext
	SpaceID         ID
	Citation        *Citation
	TargetRequestID *ID
	Kind            string
	Comment         string
}

type FeedbackResult struct {
	RequestID  ID
	FeedbackID ID
	State      string
	DraftID    *ID
}

// Feedback records suggestions only. It has no approve/publish method.
type FeedbackService interface {
	Submit(context.Context, FeedbackRequest) (FeedbackResult, error)
}

// Approval and publication remain separate; an engine's indexing status cannot
// satisfy either command. Implementations must enforce the platform state machine.
type PublicationService interface {
	Submit(context.Context, SubmitRequest) (SubmitResult, error)
	Approve(context.Context, ApprovalDecisionRequest) (Approval, error)
	Publish(context.Context, PublishRequest) (PublicationOperation, error)
	Withdraw(context.Context, WithdrawRequest) (WithdrawResult, error)
}
