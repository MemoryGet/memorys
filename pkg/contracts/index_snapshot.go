package contracts

import "context"

// Draft transfer model for projection data, not publication authority or full
// metadata migration. Published remains a projection flag.
type IndexProjection struct {
	SpaceID, KnowledgeBaseID, DocumentID, GenerationID, BackendEpochID ID
	Published                                                          bool
	Chunk                                                              Chunk
	Citation                                                           Citation
}
type IndexChunkFence struct{ GenerationID, ChunkID ID }
type IndexSnapshot struct {
	Format                   string
	SpaceID, KnowledgeBaseID ID
	Generations              []IndexDeletionState
	Records                  []IndexProjection
	ChunkFences              []IndexChunkFence
	Checksum                 Digest
}
type IndexSnapshotStore interface {
	ExportIndex(context.Context, AccessScope, []ID) (IndexSnapshot, error)
	ImportIndex(context.Context, AccessScope, IndexSnapshot) error
}
