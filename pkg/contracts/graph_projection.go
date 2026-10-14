package contracts

import (
	"context"
	"time"
)

// One KB/epoch/generation per target. Cross-KB query fusion is an application
// concern; a database's global node identifiers must not replace platform IDs.
type GraphProjectionTarget struct {
	SpaceID, KnowledgeBaseID, BackendEpochID, GenerationID                            ID
	IndexProfileDigest, ExtractionProfileDigest, OntologyDigest, SourceSnapshotDigest Digest
}

type GraphProjectionPrepareRequest struct {
	Command     CommandContext
	Target      GraphProjectionTarget
	AttemptID   ID
	RevisionIDs []ID
	Evidence    []Evidence
}

type GraphProjectionProof struct {
	ProofID, AttemptID         ID
	Target                     GraphProjectionTarget
	InputDigest                Digest
	State                      string // prepared or verified; never an authority/publication grant.
	EntityCount, RelationCount uint64
}

type GraphProjectionVerifyRequest struct {
	Request RequestContext
	Scope   AccessScope
	Proof   GraphProjectionProof
}

type GraphSourceInvalidationRequest struct {
	Command                CommandContext
	OperationID            ID
	RequestDigest          Digest // Computed from the frozen semantic operation, not a provider claim.
	Target                 GraphProjectionTarget
	DocumentID, RevisionID ID
	PublicationEpoch       uint64
}

type GraphCleanupReceipt struct {
	OperationID             ID
	OperationKind           string // source_invalidation or generation_deletion
	RequestDigest           Digest
	Target                  GraphProjectionTarget
	State                   string // queued, running, completed, failed or unknown.
	CompletedAt             *time.Time
	ProviderRecordReference ID // Controlled evidence reference, not an arbitrary URL.
}

type GraphCleanupLoadRequest struct {
	Request       RequestContext
	Scope         AccessScope
	Target        GraphProjectionTarget
	OperationID   ID
	OperationKind string
	RequestDigest Digest
}

type GraphCleanupExpectation struct {
	Target        GraphProjectionTarget
	OperationID   ID
	OperationKind string
	RequestDigest Digest
}

type GraphGenerationDeleteRequest struct {
	Command           CommandContext
	OperationID       ID
	RequestDigest     Digest
	Target            GraphProjectionTarget
	RetentionPermitID ID // Reference only; application must verify current permit.
}

// Frozen cleanup semantics, excluding temporary scope/transport/deadline data.
// SubjectID must be resolved from authenticated platform authority by the app.
type GraphCleanupSemantic struct {
	OperationID, SubjectID ID
	Kind                   string
	Target                 GraphProjectionTarget
	DocumentID, RevisionID ID
	PublicationEpoch       uint64
	RetentionPermitID      ID
}

// Derived graph lifecycle, distinct from Graph's extraction/query port.
// Implementations never activate publication pointers. Authority/retention,
// idempotency and source/attempt fencing require application and persistence.
type GraphProjectionManager interface {
	Prepare(context.Context, GraphProjectionPrepareRequest) (GraphProjectionProof, error)
	Verify(context.Context, GraphProjectionVerifyRequest) (GraphProjectionProof, error)
	InvalidateSource(context.Context, GraphSourceInvalidationRequest) (GraphCleanupReceipt, error)
	LoadCleanup(context.Context, GraphCleanupLoadRequest) (GraphCleanupReceipt, error)
	DeleteGeneration(context.Context, GraphGenerationDeleteRequest) (GraphCleanupReceipt, error)
}
