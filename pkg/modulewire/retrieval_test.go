package modulewire_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func embeddingSpec() contracts.EmbeddingSpec {
	return contracts.EmbeddingSpec{ProviderModel: "fixture", ModelRevision: "1", Dimension: 2, Distance: "cosine", Normalization: "l2", PreprocessVersion: "1"}
}

func TestEmbeddingRequestAndResultPreserveSpecAndFloat32(t *testing.T) {
	parse := parseRequest()
	call := modulewire.Call{InvocationID: "i", ModuleInstanceID: "embed-a"}
	request := contracts.EmbedRequest{Request: parse.Request, Texts: []string{"中文😀", "精度"}, Spec: embeddingSpec()}
	data, err := modulewire.EncodeEmbedRequest(call, parse.Scope, request)
	if err != nil {
		t.Fatal(err)
	}
	actualCall, scope, actual, err := modulewire.DecodeEmbedRequest(data)
	if err != nil || actualCall != call || !reflect.DeepEqual(scope, parse.Scope) || !reflect.DeepEqual(actual, request) {
		t.Fatal("embedding request changed", err)
	}
	result := contracts.EmbedResult{Spec: request.Spec, Vectors: []contracts.Vector{{0.1, 0.9}, {0.2, 0.8}}}
	data, err = modulewire.EncodeEmbedResult("req-1", "i", result)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := modulewire.DecodeEmbedResult(data, "req-1", "i", request)
	if err != nil || !reflect.DeepEqual(decoded, result) {
		t.Fatal("float32 or embedding spec changed", err)
	}
	result.Spec.ModelRevision = "other"
	data, _ = modulewire.EncodeEmbedResult("req-1", "i", result)
	if _, err := modulewire.DecodeEmbedResult(data, "req-1", "i", request); err == nil {
		t.Fatal("wrong model revision accepted")
	}
}

func retrievalRequest() contracts.RetrieveRequest {
	parse := parseRequest()
	spec := embeddingSpec()
	generationSpec := spec
	generationID := contracts.ID("gen-1")
	specDigest := contracts.Digest(strings.Repeat("e", 64))
	proof := &contracts.ReadinessProof{GenerationSetID: "set-1", AttemptID: "attempt-1", ContentDigest: contracts.Digest(strings.Repeat("d", 64)), ProfileDigest: parse.Request.ProfileDigest,
		Generations: []contracts.GenerationReadiness{{GenerationID: generationID, Channel: contracts.VectorChannel, BackendEpochID: "epoch-1", ChunkCount: 1, EmbeddingSpecDigest: &specDigest}}}
	return contracts.RetrieveRequest{Request: parse.Request, Scope: parse.Scope, Query: contracts.Query{Text: "故障", Vector: contracts.Vector{0.1, 0.9}, EmbeddingSpec: &spec},
		Publication: contracts.PublicationFilter{SpaceID: "space-1", KnowledgeBaseIDs: []contracts.ID{"kb-1"}, RevisionIDs: []contracts.ID{"rev-1"}, GenerationIDs: []contracts.ID{generationID}, SnapshotDigest: contracts.Digest(strings.Repeat("c", 64))},
		Generations: []contracts.IndexGeneration{{ID: generationID, KnowledgeBaseID: "kb-1", RevisionID: "rev-1", Channel: contracts.VectorChannel, ProfileDigest: parse.Request.ProfileDigest,
			EmbeddingSpec: &generationSpec, BackendEpochID: "epoch-1", State: "ready", Readiness: proof, BackendBinding: &contracts.BackendBinding{PlatformResourceType: "generation", PlatformResourceID: generationID,
				ModuleInstanceID: "index-a", BackendID: "opaque-1", GenerationID: &generationID, Purpose: "retrieval"}}}, BackendEpochID: "epoch-1", RouteID: "route-1", Channels: []contracts.Channel{contracts.VectorChannel}, Limit: 10}
}

func TestRetrievalRoundTripKeepsAllFiltersAndPrivateGenerationMetadata(t *testing.T) {
	request := retrievalRequest()
	call := modulewire.Call{InvocationID: "i", ModuleInstanceID: "retriever-a"}
	data, err := modulewire.EncodeRetrieveRequest(call, request)
	if err != nil {
		t.Fatal(err)
	}
	actualCall, actual, err := modulewire.DecodeRetrieveRequest(data)
	if err != nil || actualCall != call || !reflect.DeepEqual(actual, request) {
		t.Fatal("retrieval filters or generation fields lost", err)
	}
	candidates := []contracts.Candidate{{Citation: contracts.Citation{DocumentID: "doc-1", RevisionID: "rev-1", BlockID: "block-1",
		Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block-1", Locator: contracts.Locator{Precision: "coarse"}}}, ChunkID: "chunk-1", RouteID: request.RouteID,
		RetrieverID: "retriever-a", KnowledgeBaseID: "kb-1", Rank: 1, Score: 0.1, ScoreKind: "similarity", GenerationIDs: []contracts.ID{"gen-1"}, Text: "证据"}}
	data, err = modulewire.EncodeRetrieveResult("req-1", "i", candidates)
	if err != nil {
		t.Fatal(err)
	}
	actualCandidates, err := modulewire.DecodeRetrieveResult(data, "req-1", "i", request)
	if err != nil || !reflect.DeepEqual(actualCandidates, candidates) {
		t.Fatal("candidate provenance lost", err)
	}
	candidates[0].GenerationIDs = []contracts.ID{"other-generation"}
	data, _ = modulewire.EncodeRetrieveResult("req-1", "i", candidates)
	if _, err := modulewire.DecodeRetrieveResult(data, "req-1", "i", request); err == nil {
		t.Fatal("out-of-filter evidence accepted")
	}
}
