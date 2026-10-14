package contracts

import "context"

// Narrow cleanup/readback capability, not a complete retriever or IndexWriter.
type IndexDeletionState struct {
	SpaceID, KnowledgeBaseID, DocumentID, RevisionID, GenerationID, BackendEpochID ID
	Deleted                                                                        bool
	RecordCount                                                                    int
}
type IndexCleanupBackend interface {
	Delete(context.Context, IndexDeleteRequest) error
	InspectGeneration(context.Context, AccessScope, ID) (IndexDeletionState, error)
}
