package contracts

import (
	"context"
	"errors"
	"time"
)

// Internal command ports, not client authority or a physical purge interface.
type DeleteDocumentRequest struct {
	Command                 CommandContext
	DocumentID              ID
	ExpectedDocumentVersion uint64
}

type DeleteDocumentResult struct {
	OperationID, HistoryID, DocumentID, JobID ID
	DocumentVersion, PublicationEpoch         uint64
	CleanupPlanID                             *ID
	DeletedAt                                 time.Time
}

type DocumentDeletionCommand struct {
	Request                                                 DeleteDocumentRequest
	OperationID, HistoryID, PlanID, EventID, AuditID, JobID ID
	ActorDigest, CommandDigest                              Digest
	OccurredAt                                              time.Time
}

type DocumentDeletionStore interface {
	DeleteDocument(context.Context, DocumentDeletionCommand) (DeleteDocumentResult, error)
}

var ErrDocumentDeleteConflict = errors.New("document deletion conflicts with current version or command")

// Location is resolved from platform metadata, not caller ownership claims.
// Authority maps kb.delete to a resource-bound internal document.delete scope.
type DeletionTarget struct {
	DocumentID, SpaceID, KnowledgeBaseID ID
	Scope                                AccessScope
}

type DeletionAuthority interface {
	AuthorizeDocumentDeletion(context.Context, Principal, ID) (DeletionTarget, error)
}
