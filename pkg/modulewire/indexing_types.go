package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

type indexInput struct {
	GenerationSetID        contracts.ID
	Generations            []contracts.IndexGeneration
	OperationID, AttemptID contracts.ID
	FencingToken           uint64
	Chunks                 []contracts.Chunk
	Vectors                []contracts.Vector
}
type compositeInput struct {
	GenerationSetID         contracts.ID
	Generations             []contracts.IndexGeneration
	OperationID, AttemptID  contracts.ID
	FencingToken            uint64
	Chunks                  []contracts.Chunk
	ProjectionFormatVersion string
}
type GenerationBinding struct {
	GenerationID     contracts.ID
	OpaqueBackendRef string
}
type PrepareResult struct {
	Proof    contracts.ReadinessProof
	Bindings []GenerationBinding
}
type PrepareExpectation struct {
	GenerationSetID, AttemptID contracts.ID
	Generations                []contracts.IndexGeneration
	ContentDigest              contracts.Digest
	ChunkCounts                map[contracts.ID]int
	EmbeddingSpecDigests       map[contracts.ID]contracts.Digest
}
type verifyInput struct{ Proof contracts.ReadinessProof }
type deleteInput struct {
	GenerationID contracts.ID
	ChunkIDs     []contracts.ID
	Mode         string
}
type ack struct{ Completed bool }

func init() {
	register(invocation[indexInput]{}, "Context:context Input:input")
	register(invocation[compositeInput]{}, "Context:context Input:input")
	register(indexInput{}, "GenerationSetID:generation_set_id Generations:generations OperationID:operation_id AttemptID:attempt_id FencingToken:fencing_token Chunks:chunks Vectors:vectors")
	register(compositeInput{}, "GenerationSetID:generation_set_id Generations:generations OperationID:operation_id AttemptID:attempt_id FencingToken:fencing_token Chunks:chunks ProjectionFormatVersion:projection_format_version")
	register(contracts.Chunk{}, "ID:id RevisionID:revision_id Spans:spans Text:text ProfileDigest:profile_digest ContentHash:content_hash")
	register(contracts.BlockSpan{}, "BlockID:block_id Characters:characters?")
	register(response[PrepareResult]{}, "RequestID:request_id InvocationID:invocation_id Result:result")
	register(PrepareResult{}, "Proof:proof Bindings:bindings")
	register(GenerationBinding{}, "GenerationID:generation_id OpaqueBackendRef:opaque_backend_ref")
	register(invocation[verifyInput]{}, "Context:context Input:input")
	register(verifyInput{}, "Proof:proof")
	register(invocation[deleteInput]{}, "Context:context Input:input")
	register(deleteInput{}, "GenerationID:generation_id ChunkIDs:chunk_ids Mode:mode")
	register(response[ack]{}, "RequestID:request_id InvocationID:invocation_id Result:result")
	register(ack{}, "Completed:completed")
}
