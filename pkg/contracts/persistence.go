package contracts

import (
	"context"
	"io"
	"time"
)

type AuditRecord struct {
	ID          ID
	RequestID   ID
	OperationID ID
	SpaceID     ID
	ActorID     ID
	Action      Action
	ResourceIDs []ID
	OccurredAt  time.Time
}

type IdempotencyRecord struct {
	SpaceID         ID
	SubjectID       ID
	Action          Action
	Key             string
	RequestDigest   Digest
	ResultReference ID
	State           string
	ExpiresAt       time.Time
}

type MigrationJournalRecord struct {
	TransactionWatermark string
	Ordinal              uint64
	OperationID          ID
	ResourceID           ID
	ResourceVersion      uint64
	PayloadReference     BlobRef
	Checksum             Digest
	Tombstone            bool
}

type ExportRequest struct {
	Request    RequestContext
	Scope      AccessScope
	SnapshotID ID
	Watermark  string
}

type ImportRequest struct {
	Command         CommandContext
	TargetNamespace ID
	SchemaVersion   uint64
	ExpectedDigest  Digest
	ValidateOnly    bool
}

type MigrationReport struct {
	SnapshotID     ID
	Watermark      string
	RecordCount    uint64
	VerifiedDigest Digest
	Diagnostics    []Diagnostic
}

type MetadataRepository interface {
	LoadDocument(context.Context, AccessScope, ID) (Document, error)
	LoadDraft(context.Context, AccessScope, ID) (Draft, error)
	LoadRevision(context.Context, AccessScope, ID) (Revision, error)
	LoadApprovalRequest(context.Context, AccessScope, ID) (ApprovalRequest, error)
	LoadGenerationSet(context.Context, AccessScope, ID) (GenerationSet, error)
	LoadBackendEpoch(context.Context, AccessScope, ID) (BackendEpoch, error)
	SaveDocument(context.Context, AccessScope, Document, uint64) error
	SaveDraft(context.Context, AccessScope, Draft, uint64) error
	CreateRevision(context.Context, AccessScope, Revision) error
	SaveApproval(context.Context, AccessScope, Approval, uint64) error
	SaveApprovalRequest(context.Context, AccessScope, ApprovalRequest, uint64) error
	SaveGenerationSet(context.Context, AccessScope, GenerationSet) error
	SaveBackendEpoch(context.Context, AccessScope, BackendEpoch, uint64) error
	SavePublication(context.Context, AccessScope, PublicationOperation, uint64) error
	Export(context.Context, ExportRequest, io.Writer) (MigrationReport, error)
	Import(context.Context, ImportRequest, io.Reader) (MigrationReport, error)
}

type AuditRepository interface {
	Append(context.Context, AuditRecord) error
}

type OutboxRepository interface {
	Append(context.Context, EventEnvelope) error
}

type IdempotencyRepository interface {
	Load(context.Context, AccessScope, Action, string) (IdempotencyRecord, error)
	Save(context.Context, AccessScope, IdempotencyRecord) error
}

type MigrationJournalRepository interface {
	Append(context.Context, MigrationJournalRecord) error
}

type ConsumerReceipt struct {
	ConsumerID  ID
	EventID     ID
	ProcessedAt time.Time
}

type ConsumerReceiptRepository interface {
	Contains(context.Context, ID, ID) (bool, error)
	Append(context.Context, ConsumerReceipt) error
}

// Repositories are scoped to a transaction and cannot escape its callback.
// The implementation must enforce ownership and invalidate handles at completion.
type TransactionRepositories struct {
	Metadata           MetadataRepository
	Tasks              TaskRepository
	Audit              AuditRepository
	Outbox             OutboxRepository
	Idempotency        IdempotencyRepository
	Journal            MigrationJournalRepository
	ConsumerReceipts   ConsumerReceiptRepository
	PublicationHistory PublicationHistoryRepository
	CleanupPlans       CleanupPlanRepository
}

type UnitOfWork interface {
	Within(context.Context, AccessScope, func(context.Context, TransactionRepositories) error) error
}

type EvaluationRequest struct {
	Request                RequestContext
	DatasetID              ID
	DatasetDigest          Digest
	ProfileDigest          Digest
	CorpusDigest           Digest
	ImplementationVersions map[Capability]string
}

type Metric struct {
	Name   string
	Value  float64
	Unit   string
	Labels map[string]string
}

type EvaluationReport struct {
	ID                 ID
	Run                EvaluationRequest
	Metrics            []Metric
	Failures           []Diagnostic
	ArtifactReferences []ID
}

type Evaluator interface {
	Evaluate(context.Context, EvaluationRequest) (EvaluationReport, error)
}

type Observation struct {
	Request               RequestContext
	Capability            Capability
	ImplementationVersion string
	GenerationID          *ID
	Metrics               []Metric
	DegradedReasons       []Diagnostic
}

// Observer is best-effort telemetry. Required audit writes use AuditRepository
// inside UnitOfWork; sending telemetry cannot satisfy durable business auditing.
type Observer interface {
	Record(context.Context, Observation) error
}
