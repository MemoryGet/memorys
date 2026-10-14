package fusion_test

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/fusion"
	"github.com/MemoryGet/memorys/pkg/modulewire"
	"math"
	"reflect"
	"testing"
	"time"
)

func candidate(route, id string, rank int, score float64) contracts.Candidate {
	return contracts.Candidate{Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: contracts.ID(id), Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: contracts.ID(id), Locator: contracts.Locator{Precision: "coarse"}}}, ChunkID: contracts.ID(id), RouteID: contracts.ID(route), RetrieverID: contracts.ID(route + "-backend"), KnowledgeBaseID: "kb", Rank: rank, Score: score, ScoreKind: "vendor", GenerationIDs: []contracts.ID{contracts.ID(route + "-gen")}, Text: "中文证据"}
}
func request(lists ...contracts.RankedList) contracts.FuseRequest {
	return contracts.FuseRequest{Request: contracts.RequestContext{RequestID: "request", ProfileDigest: contracts.Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Deadline: time.Now().Add(time.Minute)}, Lists: lists, RankConstant: 60, Limit: 10}
}

func TestRRFUsesRanksPreservesBothOriginsAndRoundtripsRerankWire(t *testing.T) {
	a := contracts.RankedList{RouteID: "a", Candidates: []contracts.Candidate{candidate("a", "x", 1, 1e6), candidate("a", "y", 2, 1e6)}}
	b := contracts.RankedList{RouteID: "b", Candidates: []contracts.Candidate{candidate("b", "y", 1, -1e6), candidate("b", "x", 2, -1e6)}}
	req := request(a, b)
	out, err := (fusion.RRF{}).Fuse(context.Background(), req)
	if err != nil || len(out) != 2 || len(out[0].FusionOrigins) != 2 || len(out[0].GenerationIDs) != 2 || out[0].ScoreKind != "rrf" {
		t.Fatal(out, err)
	}
	other, err := (fusion.RRF{}).Fuse(context.Background(), request(b, a))
	if err != nil || !reflect.DeepEqual(out, other) {
		t.Fatal("arrival order changed result", err)
	}
	scope := contracts.AccessScope{ScopeID: "s", SubjectID: "u", Action: "knowledge.search", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: req.Request.Deadline}
	raw, err := modulewire.EncodeRerankRequest(modulewire.Call{InvocationID: "i", ModuleInstanceID: "rerank"}, scope, contracts.RerankRequest{Request: req.Request, Query: "故障", Candidates: out, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, _, decoded, err := modulewire.DecodeRerankRequest(raw)
	if err != nil || !reflect.DeepEqual(decoded.Candidates, out) {
		t.Fatal("origins lost on wire", err)
	}
}

func TestDuplicateRouteAndCandidateCannotIncreaseVotes(t *testing.T) {
	x := candidate("a", "x", 1, 0)
	duplicate := x
	duplicate.Rank = 2
	list := contracts.RankedList{RouteID: "a", Candidates: []contracts.Candidate{x, duplicate}}
	one, err := (fusion.RRF{}).Fuse(context.Background(), request(list))
	if err != nil {
		t.Fatal(err)
	}
	two, err := (fusion.RRF{}).Fuse(context.Background(), request(list, list))
	if err != nil || !reflect.DeepEqual(one, two) || len(one[0].FusionOrigins) != 1 {
		t.Fatal("duplicate increased vote", err)
	}
	if one[0].Score != 1.0/61 {
		t.Fatal("duplicate candidate contribution counted")
	}
	conflict := list
	conflict.Candidates = []contracts.Candidate{candidate("a", "z", 1, 0)}
	if _, err := (fusion.RRF{}).Fuse(context.Background(), request(list, conflict)); err == nil {
		t.Fatal("different lists under one route accepted")
	}
}

func TestConflictingEvidenceAndRefusionRejected(t *testing.T) {
	a := contracts.RankedList{RouteID: "a", Candidates: []contracts.Candidate{candidate("a", "x", 1, 0)}}
	b := contracts.RankedList{RouteID: "b", Candidates: []contracts.Candidate{candidate("b", "x", 1, 0)}}
	b.Candidates[0].Text = "另一个正文"
	if _, err := (fusion.RRF{}).Fuse(context.Background(), request(a, b)); err == nil {
		t.Fatal("same source changed text accepted")
	}
	one, err := (fusion.RRF{}).Fuse(context.Background(), request(a))
	if err != nil {
		t.Fatal(err)
	}
	a.Candidates = one
	if _, err := (fusion.RRF{}).Fuse(context.Background(), request(a)); err == nil {
		t.Fatal("refused fused result as raw route")
	}
}

func TestBoundsCancellationAndOutputCopies(t *testing.T) {
	a := contracts.RankedList{RouteID: "a", Candidates: []contracts.Candidate{candidate("a", "x", 1, 0)}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (fusion.RRF{}).Fuse(ctx, request(a)); err == nil {
		t.Fatal("cancellation ignored")
	}
	for _, mutate := range []func(*contracts.FuseRequest){func(r *contracts.FuseRequest) { r.RankConstant = 0 }, func(r *contracts.FuseRequest) { r.Limit = 0 }, func(r *contracts.FuseRequest) { r.Request.Deadline = time.Now().Add(-time.Second) }, func(r *contracts.FuseRequest) { r.Lists[0].Candidates[0].Score = math.Inf(1) }, func(r *contracts.FuseRequest) { r.Lists[0].Candidates[0].Rank = 0 }} {
		copyList := a
		copyList.Candidates = append([]contracts.Candidate(nil), a.Candidates...)
		r := request(copyList)
		mutate(&r)
		if _, err := (fusion.RRF{}).Fuse(context.Background(), r); err == nil {
			t.Fatal("invalid bounded input accepted")
		}
	}
	out, err := (fusion.RRF{}).Fuse(context.Background(), request(a))
	if err != nil {
		t.Fatal(err)
	}
	out[0].GenerationIDs[0] = "changed"
	out[0].FusionOrigins[0].GenerationIDs[0] = "changed"
	if a.Candidates[0].GenerationIDs[0] != "a-gen" {
		t.Fatal("output aliases input")
	}
}
