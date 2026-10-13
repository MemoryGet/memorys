package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func TestIndexInspectionDoesNotActivateUnmatchedResults(t *testing.T) {
	var baseline struct{ Request json.RawMessage }
	json.Unmarshal(fixture(t), &baseline)
	call, original, err := modulewire.DecodeParseRequest(baseline.Request)
	if err != nil {
		t.Fatal(err)
	}
	original.Scope.KnowledgeBaseIDs = []contracts.ID{"kb"}
	generation := contracts.IndexGeneration{ID: "gen", KnowledgeBaseID: "kb", RevisionID: "rev", Channel: contracts.KeywordChannel, ProfileDigest: original.Request.ProfileDigest, BackendEpochID: "epoch", State: "building"}
	chunk := contracts.Chunk{ID: "chunk", RevisionID: "rev", Spans: []contracts.BlockSpan{{BlockID: "block"}}, Text: "证据", ProfileDigest: original.Request.ProfileDigest, ContentHash: original.Request.ProfileDigest}
	request := contracts.IndexPrepareRequest{Request: original.Request, Scope: original.Scope, GenerationSetID: "set", Generations: []contracts.IndexGeneration{generation},
		OperationID: "op", AttemptID: "attempt", FencingToken: 1, Chunks: []contracts.Chunk{chunk}, Vectors: []contracts.Vector{}}
	proof := contracts.ReadinessProof{GenerationSetID: "set", AttemptID: "attempt", ContentDigest: original.Request.ProfileDigest, ProfileDigest: original.Request.ProfileDigest,
		Generations: []contracts.GenerationReadiness{{GenerationID: "gen", Channel: contracts.KeywordChannel, BackendEpochID: "epoch", ChunkCount: 1}}}
	result := modulewire.PrepareResult{Proof: proof, Bindings: []modulewire.GenerationBinding{{GenerationID: "gen", OpaqueBackendRef: "private-fixture"}}}
	expectation, _ := json.Marshal(map[string]any{"content_digest": original.Request.ProfileDigest, "chunk_counts": map[string]int{"gen": 1}, "embedding_spec_digests": map[string]string{}})
	response, err := modulewire.EncodePrepareResult("r", "i", result)
	if err != nil {
		t.Fatal(err)
	}
	data, err := modulewire.EncodeIndexPrepareRequest(call, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectIndex(&bytes.Buffer{}, "index_writer_prepare", data, response, expectation); err != nil {
		t.Fatal(err)
	}
	if err := inspectIndex(&bytes.Buffer{}, "index_writer_prepare", data, response, []byte(`{}`)); err == nil {
		t.Fatal("missing authority accepted")
	}
	composite := contracts.CompositePrepareRequest{Request: request.Request, Scope: request.Scope, GenerationSetID: request.GenerationSetID, Generations: request.Generations,
		OperationID: request.OperationID, AttemptID: request.AttemptID, FencingToken: request.FencingToken, Chunks: request.Chunks, ProjectionFormatVersion: "1.0"}
	data, err = modulewire.EncodeCompositePrepareRequest(call, composite)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectIndex(&bytes.Buffer{}, "composite_ingest_search_prepare_projection", data, response, expectation); err != nil {
		t.Fatal(err)
	}
	ack, _ := modulewire.EncodeIndexAck("r", "i")
	data, err = modulewire.EncodeIndexVerifyRequest(call, request.Request, request.Scope, proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectIndex(&bytes.Buffer{}, "index_writer_verify", data, ack, nil); err != nil {
		t.Fatal(err)
	}
	data, err = modulewire.EncodeIndexDeleteRequest(call, contracts.IndexDeleteRequest{Request: request.Request, Scope: request.Scope, GenerationID: "gen", ChunkIDs: []contracts.ID{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectIndex(&bytes.Buffer{}, "index_writer_delete", data, ack, nil); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"index_writer_prepare", "composite_ingest_search_prepare_projection", "index_writer_verify", "index_writer_delete", "unknown"} {
		if err := inspectIndex(&bytes.Buffer{}, operation, []byte(`{}`), ack, nil); err == nil {
			t.Fatal("invalid inspection accepted")
		}
	}
}
