package retrievalplan_test

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/fusion"
	"github.com/MemoryGet/memorys/pkg/retrievalplan"
)

func mergeFixture(t *testing.T) (retrievalplan.Route, []retrievalplan.PartitionResult) {
	t.Helper()
	p, err := retrievalplan.Build(profile(), inventory(), limits)
	if err != nil {
		t.Fatal(err)
	}
	r := p.Routes[1]
	var results []retrievalplan.PartitionResult
	for _, call := range r.Calls {
		result := retrievalplan.PartitionResult{Target: call.Target}
		for i := 0; i < call.Limit; i++ {
			result.Candidates = append(result.Candidates, contracts.Candidate{RouteID: r.ID, RetrieverID: call.RetrieverID, KnowledgeBaseID: call.Target.KnowledgeBaseID, ChunkID: contracts.ID(string(call.Target.KnowledgeBaseID) + string(rune('x'+i))), Rank: i + 1, Score: float64(i) * 1e6, ScoreKind: "vendor", GenerationIDs: []contracts.ID{"gen"}, Text: "中文证据", Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse", ParagraphPath: []int{1}}}}})
		}
		results = append(results, result)
	}
	return r, results
}

func TestMergeStableInterleaveAndCopies(t *testing.T) {
	r, results := mergeFixture(t)
	merged, err := retrievalplan.Merge(context.Background(), r, results)
	if err != nil {
		t.Fatal(err)
	}
	want := []contracts.ID{"kb-a", "kb-b", "kb-c", "kb-a"}
	for i, c := range merged.List.Candidates {
		if c.KnowledgeBaseID != want[i] || c.Rank != i+1 {
			t.Fatal("incorrect global ranking", merged)
		}
	}
	if merged.Origins[3].OriginalRank != 2 || merged.Origins[3].GlobalRank != 4 {
		t.Fatal("lost local rank")
	}
	results[0], results[2] = results[2], results[0]
	r.Calls[0], r.Calls[2] = r.Calls[2], r.Calls[0]
	again, err := retrievalplan.Merge(context.Background(), r, results)
	if err != nil || !reflect.DeepEqual(merged, again) {
		t.Fatal("arrival order changed merge", err)
	}
	results[0].Candidates[0].GenerationIDs[0] = "changed"
	results[0].Candidates[0].Citation.Source.Locator.ParagraphPath[0] = 99
	if merged.List.Candidates[2].GenerationIDs[0] != "gen" || merged.List.Candidates[2].Citation.Source.Locator.ParagraphPath[0] != 1 {
		t.Fatal("mutable input escaped")
	}
}
func TestMergedKBsContributeOneRouteVote(t *testing.T) {
	r, results := mergeFixture(t)
	m, err := retrievalplan.Merge(context.Background(), r, results)
	if err != nil {
		t.Fatal(err)
	}
	out, err := (fusion.RRF{}).Fuse(context.Background(), contracts.FuseRequest{Request: contracts.RequestContext{Deadline: time.Now().Add(time.Minute)}, Lists: []contracts.RankedList{m.List}, RankConstant: 60, Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range out {
		if len(c.FusionOrigins) != 1 || c.Score != 1/float64(61+i) {
			t.Fatal("KB fanout added votes", c)
		}
	}
}
func TestMergeRejectsMissingDuplicateWrongTargetAndQuota(t *testing.T) {
	for _, mutate := range []func(*retrievalplan.Route, *[]retrievalplan.PartitionResult){
		func(_ *retrievalplan.Route, r *[]retrievalplan.PartitionResult) { *r = (*r)[:2] },
		func(_ *retrievalplan.Route, r *[]retrievalplan.PartitionResult) { (*r)[1] = (*r)[0] },
		func(_ *retrievalplan.Route, r *[]retrievalplan.PartitionResult) {
			(*r)[0].Target.BackendEpochID = "wrong"
		},
		func(_ *retrievalplan.Route, r *[]retrievalplan.PartitionResult) {
			(*r)[1].Candidates = append((*r)[1].Candidates, (*r)[1].Candidates[0])
		},
		func(r *retrievalplan.Route, _ *[]retrievalplan.PartitionResult) { r.Limit++ },
	} {
		r, res := mergeFixture(t)
		mutate(&r, &res)
		if out, err := retrievalplan.Merge(context.Background(), r, res); err == nil || len(out.List.Candidates) != 0 {
			t.Fatal("invalid partition accepted")
		}
	}
}
func TestMergeRejectsInjectedCandidateFields(t *testing.T) {
	for _, mutate := range []func(*contracts.Candidate){
		func(c *contracts.Candidate) { c.RouteID = "injected" }, func(c *contracts.Candidate) { c.RetrieverID = "wrong" }, func(c *contracts.Candidate) { c.KnowledgeBaseID = "wrong" }, func(c *contracts.Candidate) { c.FusionOrigins = []contracts.FusionOrigin{} }, func(c *contracts.Candidate) { c.GenerationIDs = nil }, func(c *contracts.Candidate) { c.Rank = 0 },
	} {
		r, res := mergeFixture(t)
		mutate(&res[0].Candidates[0])
		if _, err := retrievalplan.Merge(context.Background(), r, res); err == nil {
			t.Fatal("invalid candidate accepted")
		}
	}
}
func TestMergeEmptyResultsAndCancellation(t *testing.T) {
	r, res := mergeFixture(t)
	for i := range res {
		res[i].Candidates = nil
	}
	if out, err := retrievalplan.Merge(context.Background(), r, res); err != nil || len(out.List.Candidates) != 0 || out.List.RouteID != r.ID {
		t.Fatal(out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := retrievalplan.Merge(ctx, r, res); err == nil {
		t.Fatal("cancelled merge succeeded")
	}
	if _, err := retrievalplan.Merge(nil, r, res); err == nil {
		t.Fatal("nil context succeeded")
	}
}

func TestMergeRejectsRankOrderNonfiniteScoreAndByteBudget(t *testing.T) {
	for _, change := range []func([]retrievalplan.PartitionResult){
		func(r []retrievalplan.PartitionResult) { r[0].Candidates[1].Rank = 1 },
		func(r []retrievalplan.PartitionResult) { r[0].Candidates[0].Score = math.Inf(1) },
		func(r []retrievalplan.PartitionResult) { r[0].Candidates[0].Text = strings.Repeat("x", (16<<20)+1) },
	} {
		r, res := mergeFixture(t)
		change(res)
		if out, err := retrievalplan.Merge(context.Background(), r, res); err == nil || len(out.List.Candidates) != 0 {
			t.Fatal("invalid rank/score/size produced output")
		}
	}
	if out, err := retrievalplan.Merge(context.Background(), retrievalplan.Route{ID: "empty", Limit: 2, Required: true}, nil); err != nil || out.List.RouteID != "empty" || len(out.List.Candidates) != 0 {
		t.Fatal("no eligible target should be explicit empty result", err)
	}
}
