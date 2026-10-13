package modulewire_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func indexRequest() contracts.IndexPrepareRequest {
	parse := parseRequest()
	spec := embeddingSpec()
	return contracts.IndexPrepareRequest{Request: parse.Request, Scope: parse.Scope, GenerationSetID: "set", OperationID: "op", AttemptID: "attempt",
		FencingToken: 9007199254740993, Generations: []contracts.IndexGeneration{{ID: "gen", KnowledgeBaseID: "kb-1", RevisionID: "rev", Channel: contracts.VectorChannel,
			ProfileDigest: parse.Request.ProfileDigest, EmbeddingSpec: &spec, BackendEpochID: "epoch", State: "building"}},
		Chunks: []contracts.Chunk{{ID: "chunk", RevisionID: "rev", Spans: []contracts.BlockSpan{{BlockID: "block", Characters: &contracts.CharacterRange{Start: 0, End: 3}}},
			Text: "中文😀", ProfileDigest: parse.Request.ProfileDigest, ContentHash: contracts.Digest(strings.Repeat("b", 64))}}, Vectors: []contracts.Vector{{0.1, 0.9}}}
}

func TestIndexAndCompositeRequestsKeepAttemptFenceAndSourceSpans(t *testing.T) {
	request := indexRequest()
	call := modulewire.Call{InvocationID: "i", ModuleInstanceID: "index-a"}
	data, err := modulewire.EncodeIndexPrepareRequest(call, request)
	if err != nil {
		t.Fatal(err)
	}
	actualCall, actual, err := modulewire.DecodeIndexPrepareRequest(data)
	if err != nil || actualCall != call || !reflect.DeepEqual(actual, request) {
		t.Fatal("index fields lost", err)
	}
	composite := contracts.CompositePrepareRequest{Request: request.Request, Scope: request.Scope, GenerationSetID: request.GenerationSetID,
		Generations: request.Generations, OperationID: request.OperationID, AttemptID: request.AttemptID, FencingToken: request.FencingToken,
		Chunks: request.Chunks, ProjectionFormatVersion: "1.0"}
	data, err = modulewire.EncodeCompositePrepareRequest(call, composite)
	if err != nil {
		t.Fatal(err)
	}
	_, actualComposite, err := modulewire.DecodeCompositePrepareRequest(data)
	if err != nil || !reflect.DeepEqual(actualComposite, composite) {
		t.Fatal("composite fields lost", err)
	}
	bad := strings.Replace(string(data), `"projection_format_version":"1.0"`, `"projection_format_version":"1.0","vectors":[[0.1,0.9]]`, 1)
	if _, _, err := modulewire.DecodeCompositePrepareRequest([]byte(bad)); err == nil {
		t.Fatal("composite accepted external vectors")
	}
}

func TestPrepareResultMatchesAuthoritativeExpectedTargets(t *testing.T) {
	request := indexRequest()
	digest := contracts.Digest(strings.Repeat("c", 64))
	specDigest := contracts.Digest(strings.Repeat("d", 64))
	result := modulewire.PrepareResult{Proof: contracts.ReadinessProof{GenerationSetID: "set", AttemptID: "attempt", ContentDigest: digest, ProfileDigest: request.Generations[0].ProfileDigest,
		Generations: []contracts.GenerationReadiness{{GenerationID: "gen", Channel: contracts.VectorChannel, BackendEpochID: "epoch", ChunkCount: 1, EmbeddingSpecDigest: &specDigest}}},
		Bindings: []modulewire.GenerationBinding{{GenerationID: "gen", OpaqueBackendRef: "opaque"}}}
	expected := modulewire.PrepareExpectation{GenerationSetID: "set", AttemptID: "attempt", Generations: request.Generations, ContentDigest: digest,
		ChunkCounts: map[contracts.ID]int{"gen": 1}, EmbeddingSpecDigests: map[contracts.ID]contracts.Digest{"gen": specDigest}}
	data, err := modulewire.EncodePrepareResult("req-1", "i", result)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := modulewire.DecodePrepareResult(data, "req-1", "i", expected)
	if err != nil || !reflect.DeepEqual(actual, result) {
		t.Fatal("prepare result changed", err)
	}
	for _, mutation := range []func(*modulewire.PrepareExpectation){
		func(e *modulewire.PrepareExpectation) { e.ContentDigest = "bad-digest" },
		func(e *modulewire.PrepareExpectation) { e.ChunkCounts = map[contracts.ID]int{"gen": 2} },
		func(e *modulewire.PrepareExpectation) { e.ChunkCounts = nil },
		func(e *modulewire.PrepareExpectation) { e.EmbeddingSpecDigests = nil },
		func(e *modulewire.PrepareExpectation) { e.Generations = append(e.Generations, e.Generations[0]) },
	} {
		bad := expected; mutation(&bad)
		if _, err := modulewire.DecodePrepareResult(data, "req-1", "i", bad); err == nil { t.Fatal("invalid authoritative expectation accepted") }
	}
	result.Proof.AttemptID = "old-attempt"
	data, _ = modulewire.EncodePrepareResult("req-1", "i", result)
	if _, err := modulewire.DecodePrepareResult(data, "req-1", "i", expected); err == nil {
		t.Fatal("stale attempt proof accepted")
	}
}
