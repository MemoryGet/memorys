package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func TestRetrievalInspectionDispatchAndFailures(t *testing.T) {
	var baseline struct{ Request json.RawMessage }
	if err := json.Unmarshal(fixture(t), &baseline); err != nil {
		t.Fatal(err)
	}
	call, original, err := modulewire.DecodeParseRequest(baseline.Request)
	if err != nil {
		t.Fatal(err)
	}
	spec := contracts.EmbeddingSpec{ProviderModel: "fixture", ModelRevision: "1", Dimension: 2, Distance: "cosine", Normalization: "l2", PreprocessVersion: "1"}
	request, err := modulewire.EncodeEmbedRequest(call, original.Scope, contracts.EmbedRequest{Request: original.Request, Texts: []string{"a"}, Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	response, err := modulewire.EncodeEmbedResult("r", "i", contracts.EmbedResult{Spec: spec, Vectors: []contracts.Vector{{0.1, 0.9}}})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := inspectRetrieval(&output, "embedder_embed", request, response); err != nil || !json.Valid(output.Bytes()) {
		t.Fatal(err)
	}
	for _, operation := range []string{"embedder_embed", "retriever_search", "reranker_rerank", "unknown"} {
		if err := inspectRetrieval(&output, operation, []byte(`{}`), response); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if err := inspectRetrieval(&output, "embedder_embed", request, []byte(`{}`)); err == nil {
		t.Fatal("invalid response accepted")
	}
	original.Scope.KnowledgeBaseIDs = []contracts.ID{"kb"}
	citation := contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}
	candidate := contracts.Candidate{Citation: citation, ChunkID: "chunk", RouteID: "route", RetrieverID: "retriever", KnowledgeBaseID: "kb", Rank: 1, ScoreKind: "rank", GenerationIDs: []contracts.ID{"gen"}, Text: "证据"}
	retrieve := contracts.RetrieveRequest{Request: original.Request, Scope: original.Scope, Query: contracts.Query{Text: "query"},
		Publication:    contracts.PublicationFilter{SpaceID: original.Scope.SpaceID, KnowledgeBaseIDs: []contracts.ID{"kb"}, RevisionIDs: []contracts.ID{"rev"}, GenerationIDs: []contracts.ID{"gen"}, SnapshotDigest: original.Request.ProfileDigest},
		Generations:    []contracts.IndexGeneration{{ID: "gen", KnowledgeBaseID: "kb", RevisionID: "rev", Channel: contracts.KeywordChannel, ProfileDigest: original.Request.ProfileDigest, BackendEpochID: "epoch", State: "ready"}},
		BackendEpochID: "epoch", RouteID: "route", Channels: []contracts.Channel{contracts.KeywordChannel}, Limit: 1}
	request, err = modulewire.EncodeRetrieveRequest(call, retrieve)
	if err != nil {
		t.Fatal(err)
	}
	response, err = modulewire.EncodeRetrieveResult("r", "i", []contracts.Candidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectRetrieval(&output, "retriever_search", request, response); err != nil {
		t.Fatal(err)
	}
	if err := inspectRetrieval(&output, "retriever_search", request, []byte(`{}`)); err == nil {
		t.Fatal("invalid retrieval result accepted")
	}
	rerank := contracts.RerankRequest{Request: original.Request, Query: "query", Candidates: []contracts.Candidate{candidate}, Limit: 1}
	request, err = modulewire.EncodeRerankRequest(call, original.Scope, rerank)
	if err != nil {
		t.Fatal(err)
	}
	response, err = modulewire.EncodeRerankResult("r", "i", []contracts.Evidence{{Citation: citation, SourceExcerpt: candidate.Text, Rank: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectRetrieval(&output, "reranker_rerank", request, response); err != nil {
		t.Fatal(err)
	}
	if err := inspectRetrieval(&output, "reranker_rerank", request, []byte(`{}`)); err == nil {
		t.Fatal("invalid reranking result accepted")
	}
}
