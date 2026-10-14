package retrievalplan_test

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/fusion"
	"github.com/MemoryGet/memorys/pkg/retrievalplan"
	"reflect"
	"strings"
	"testing"
	"time"
)

func refinedMergeFixture(t *testing.T) (retrievalplan.RefinementRoute, []retrievalplan.RefinementResult) {
	t.Helper()
	q, i, p := refinementFixture()
	plan, e := retrievalplan.BuildRefinement(q, i, retrievalplan.Limits{MaxCalls: 10, MaxParallel: 2}, p)
	if e != nil {
		t.Fatal(e)
	}
	route := plan.Routes[1]
	var results []retrievalplan.RefinementResult
	for _, planned := range route.Calls {
		call := planned.Call
		kb := string(call.Target.KnowledgeBaseID)
		candidate := contracts.Candidate{RouteID: route.ID, RetrieverID: call.RetrieverID, KnowledgeBaseID: call.Target.KnowledgeBaseID, ChunkID: contracts.ID(kb + "-chunk"), Rank: 1, Score: float64(planned.QueryIndex) * 1e6, ScoreKind: "vendor", GenerationIDs: []contracts.ID{contracts.ID(kb + "-gen")}, Text: "知识证据", Citation: contracts.Citation{DocumentID: contracts.ID(kb + "-doc"), RevisionID: contracts.ID(kb + "-rev"), BlockID: contracts.ID(kb + "-block"), Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: contracts.ID(kb + "-block"), Locator: contracts.Locator{Precision: "coarse", ParagraphPath: []int{1}}}}}
		results = append(results, retrievalplan.RefinementResult{QueryIndex: planned.QueryIndex, Result: retrievalplan.PartitionResult{Target: call.Target, Candidates: []contracts.Candidate{candidate}}})
	}
	return route, results
}

func TestRefinedMergeStableQueryInterleaveAndTrace(t *testing.T) {
	r, res := refinedMergeFixture(t)
	out, e := retrievalplan.MergeRefinement(context.Background(), r, res)
	if e != nil || len(out.List.Candidates) != 6 {
		t.Fatal(e)
	}
	for i, origin := range out.Origins {
		if origin.QueryIndex != i%2 || origin.GlobalRank != i+1 || origin.OriginalRank != 1 {
			t.Fatal("wrong variant trace")
		}
	}
	for a, b := 0, len(res)-1; a < b; a, b = a+1, b-1 {
		res[a], res[b] = res[b], res[a]
	}
	again, e := retrievalplan.MergeRefinement(context.Background(), r, res)
	if e != nil || !reflect.DeepEqual(out, again) {
		t.Fatal("arrival order changed result", e)
	}
	res[0].Result.Candidates[0].GenerationIDs[0] = "changed"
	if out.List.Candidates[5].GenerationIDs[0] == "changed" {
		t.Fatal("output aliased input")
	}
}

func TestRefinedMergeUsesOneRouteVoteAfterActualRRF(t *testing.T) {
	r, res := refinedMergeFixture(t)
	out, e := retrievalplan.MergeRefinement(context.Background(), r, res)
	if e != nil {
		t.Fatal(e)
	}
	fused, e := (fusion.RRF{}).Fuse(context.Background(), contracts.FuseRequest{Request: contracts.RequestContext{Deadline: time.Now().Add(time.Minute)}, Lists: []contracts.RankedList{out.List}, RankConstant: 60, Limit: 10})
	if e != nil || len(fused) != 3 {
		t.Fatal(e, len(fused))
	}
	for i, c := range fused {
		if len(c.FusionOrigins) != 1 || c.Score != 1/float64(61+2*i) {
			t.Fatal("query variants increased votes", c.Score)
		}
	}
}

func TestRefinedMergeRejectsMissingDuplicateUnknownAndMixedBackend(t *testing.T) {
	for _, mutate := range []func(*retrievalplan.RefinementRoute, *[]retrievalplan.RefinementResult){func(_ *retrievalplan.RefinementRoute, res *[]retrievalplan.RefinementResult) { *res = (*res)[:5] }, func(_ *retrievalplan.RefinementRoute, res *[]retrievalplan.RefinementResult) { (*res)[1] = (*res)[0] }, func(_ *retrievalplan.RefinementRoute, res *[]retrievalplan.RefinementResult) {
		(*res)[0].QueryIndex = 3
	}, func(r *retrievalplan.RefinementRoute, _ *[]retrievalplan.RefinementResult) {
		r.Calls[3].Call.RetrieverID = "other"
	}, func(r *retrievalplan.RefinementRoute, _ *[]retrievalplan.RefinementResult) { r.Limit++ }} {
		r, res := refinedMergeFixture(t)
		mutate(&r, &res)
		out, e := retrievalplan.MergeRefinement(context.Background(), r, res)
		if e == nil || len(out.List.Candidates) != 0 {
			t.Fatal("invalid refinement partially merged")
		}
	}
}

func TestRefinedMergeCancellationAndExplicitEmptyRoute(t *testing.T) {
	r, res := refinedMergeFixture(t)
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := retrievalplan.MergeRefinement(c, r, res); e == nil {
		t.Fatal("cancel ignored")
	}
	out, e := retrievalplan.MergeRefinement(context.Background(), retrievalplan.RefinementRoute{ID: "empty", Limit: 2, Required: true}, nil)
	if e != nil || out.List.RouteID != "empty" || len(out.List.Candidates) != 0 {
		t.Fatal(e)
	}
}

func TestRefinedMergeSharesByteBudgetAcrossVariants(t *testing.T) {
	r, res := refinedMergeFixture(t)
	for i := range res {
		res[i].Result.Candidates[0].Text = strings.Repeat("x", 3<<20)
	}
	out, err := retrievalplan.MergeRefinement(context.Background(), r, res)
	if err != retrievalplan.ErrInvalid || len(out.List.Candidates) != 0 {
		t.Fatal("variant byte budget expanded", len(out.List.Candidates), err)
	}
}

func TestRefinedConflictingEvidenceCannotPassFusion(t *testing.T) {
	r, res := refinedMergeFixture(t)
	res[3].Result.Candidates[0].Text = "同一来源冲突正文"
	merged, err := retrievalplan.MergeRefinement(context.Background(), r, res)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (fusion.RRF{}).Fuse(context.Background(), contracts.FuseRequest{Request: contracts.RequestContext{Deadline: time.Now().Add(time.Minute)}, Lists: []contracts.RankedList{merged.List}, RankConstant: 60, Limit: 10})
	if err == nil {
		t.Fatal("cross-query source conflict reached final fused evidence")
	}
}
