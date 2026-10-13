package modulewire_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func TestRetrievalRejectsEmptyBroadenedOrMismatchedTargets(t *testing.T) {
	for _, change := range []func(*contracts.RetrieveRequest){
		func(r *contracts.RetrieveRequest) { r.Publication.GenerationIDs = nil },
		func(r *contracts.RetrieveRequest) { r.Publication.KnowledgeBaseIDs = []contracts.ID{"other-kb"} },
		func(r *contracts.RetrieveRequest) { r.Publication.SpaceID = "other-space" },
		func(r *contracts.RetrieveRequest) { r.Generations[0].BackendEpochID = "old-epoch" },
		func(r *contracts.RetrieveRequest) { r.Query.EmbeddingSpec.Dimension = 3 },
		func(r *contracts.RetrieveRequest) { r.Query.EmbeddingSpec.ModelRevision = "other-model" },
		func(r *contracts.RetrieveRequest) { r.Channels = []contracts.Channel{contracts.KeywordChannel} },
		func(r *contracts.RetrieveRequest) { r.Generations[0].EmbeddingSpec = nil },
		func(r *contracts.RetrieveRequest) { r.Generations = append(r.Generations, r.Generations[0]) },
	} {
		request := retrievalRequest()
		change(&request)
		if _, err := modulewire.EncodeRetrieveRequest(modulewire.Call{InvocationID: "i", ModuleInstanceID: "m"}, request); err == nil {
			t.Fatal("unsafe retrieval target encoded")
		}
	}
}

func TestEmbeddingDimensionsCountsAndUnderflowAreRejected(t *testing.T) {
	request := contracts.EmbedRequest{Texts: []string{"a"}, Spec: embeddingSpec()}
	for _, result := range []contracts.EmbedResult{{Spec: request.Spec}, {Spec: request.Spec, Vectors: []contracts.Vector{{0.1}}}} {
		data, err := modulewire.EncodeEmbedResult("r", "i", result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := modulewire.DecodeEmbedResult(data, "r", "i", request); err == nil {
			t.Fatal("incorrect vector shape accepted")
		}
	}
	data, _ := modulewire.EncodeEmbedResult("r", "i", contracts.EmbedResult{Spec: request.Spec, Vectors: []contracts.Vector{{0.1, 0.9}}})
	for _, value := range []string{"1e100", "1e-100", "1e-1000"} {
		invalid := strings.Replace(string(data), "[0.1,0.9]", "["+value+",0.9]", 1)
		if _, err := modulewire.DecodeEmbedResult([]byte(invalid), "r", "i", request); err == nil {
			t.Fatal("overflow or zero underflow accepted")
		}
	}
}

func TestRerankerCannotInventCitationOrText(t *testing.T) {
	parse := parseRequest()
	call := modulewire.Call{InvocationID: "i", ModuleInstanceID: "rerank-a"}
	citation := contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block",
		Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}
	request := contracts.RerankRequest{Request: parse.Request, Query: "故障", Candidates: []contracts.Candidate{{Citation: citation, Text: "原文证据", Rank: 1,
		ChunkID: "chunk", RouteID: "route", RetrieverID: "retriever", KnowledgeBaseID: "kb-1", ScoreKind: "similarity", GenerationIDs: []contracts.ID{"gen"}}}, Limit: 1}
	data, err := modulewire.EncodeRerankRequest(call, parse.Scope, request)
	if err != nil {
		t.Fatal(err)
	}
	_, _, actual, err := modulewire.DecodeRerankRequest(data)
	if err != nil || !reflect.DeepEqual(actual, request) {
		t.Fatal("rerank request changed", err)
	}
	evidence := []contracts.Evidence{{Citation: citation, SourceExcerpt: "证据", Rank: 1}}
	data, _ = modulewire.EncodeRerankResult("r", "i", evidence)
	if _, err := modulewire.DecodeRerankResult(data, "r", "i", request); err != nil {
		t.Fatal(err)
	}
	evidence[0].SourceExcerpt = "无来源新事实"
	data, _ = modulewire.EncodeRerankResult("r", "i", evidence)
	if _, err := modulewire.DecodeRerankResult(data, "r", "i", request); err == nil {
		t.Fatal("invented text accepted")
	}
}
