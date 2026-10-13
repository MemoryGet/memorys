package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

type invocation[T any] struct {
	Context wireContext
	Input   T
}
type response[T any] struct {
	RequestID, InvocationID contracts.ID
	Result                  T
}
type embedInput struct {
	Texts []string
	Spec  contracts.EmbeddingSpec
}
type retrieveInput struct {
	Query          contracts.Query
	Publication    contracts.PublicationFilter
	Generations    []contracts.IndexGeneration
	BackendEpochID contracts.ID
	RouteID        contracts.ID
	Channels       []contracts.Channel
	Limit          int
}
type retrieveResult struct{ Candidates []contracts.Candidate }

func init() {
	register(invocation[embedInput]{}, "Context:context Input:input")
	register(response[contracts.EmbedResult]{}, "RequestID:request_id InvocationID:invocation_id Result:result")
	register(embedInput{}, "Texts:texts Spec:spec")
	register(contracts.EmbedResult{}, "Vectors:vectors Spec:spec")
	register(contracts.EmbeddingSpec{}, "ProviderModel:provider_model ModelRevision:model_revision Dimension:dimension Distance:distance Normalization:normalization PreprocessVersion:preprocess_version")
	register(invocation[retrieveInput]{}, "Context:context Input:input")
	register(response[retrieveResult]{}, "RequestID:request_id InvocationID:invocation_id Result:result")
	register(retrieveInput{}, "Query:query Publication:publication Generations:generations BackendEpochID:backend_epoch_id RouteID:route_id Channels:channels Limit:limit")
	register(retrieveResult{}, "Candidates:candidates")
	register(contracts.Query{}, "Text:text Vector:vector? EmbeddingSpec:embedding_spec?")
	register(contracts.PublicationFilter{}, "SpaceID:space_id KnowledgeBaseIDs:kb_ids RevisionIDs:revision_ids GenerationIDs:generation_ids SnapshotDigest:snapshot_digest")
	register(contracts.IndexGeneration{}, "ID:id KnowledgeBaseID:kb_id RevisionID:revision_id Channel:channel ProfileDigest:profile_digest EmbeddingSpec:embedding_spec? BackendEpochID:backend_epoch_id State:state BackendBinding:backend_binding? Readiness:readiness?")
	register(contracts.BackendBinding{}, "PlatformResourceType:platform_resource_type PlatformResourceID:platform_resource_id ModuleInstanceID:module_instance_id BackendID:backend_id GenerationID:generation_id? Purpose:purpose")
	register(contracts.ReadinessProof{}, "GenerationSetID:generation_set_id AttemptID:attempt_id ContentDigest:content_digest ProfileDigest:profile_digest Generations:generations")
	register(contracts.GenerationReadiness{}, "GenerationID:generation_id Channel:channel BackendEpochID:backend_epoch_id ChunkCount:chunk_count EmbeddingSpecDigest:embedding_spec_digest?")
	register(contracts.Candidate{}, "Citation:citation ChunkID:chunk_id RouteID:route_id RetrieverID:retriever_id KnowledgeBaseID:kb_id Rank:rank Score:score ScoreKind:score_kind GenerationIDs:generation_ids Text:text")
	register(contracts.Citation{}, "DocumentID:document_id RevisionID:revision_id BlockID:block_id Source:source_anchor")
}
