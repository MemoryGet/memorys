package retrievalplan_test

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/retrievalplan"
	"testing"
)

func refinementFixture() (contracts.QueryProfile, map[contracts.ID][]retrievalplan.Target, contracts.QueryPlan) {
	q := profile()
	q.Policy.CandidateLimit = 10
	q.Routes[0].CandidateLimit = 6
	q.Routes[1].CandidateLimit = 4
	q.Reflection = contracts.ReflectionPolicy{Enabled: true, MaxExtraRounds: 2, MaxPlanQueries: 2, TotalBudgetMS: 30000}
	return q, inventory(), contracts.QueryPlan{Queries: []contracts.PlannedQuery{{Text: "故障表现", RouteIDs: []contracts.ID{"vector", "mixed"}}, {Text: "维修步骤", RouteIDs: []contracts.ID{"vector", "mixed"}}}}
}

func TestRefinementSharesRouteQuotaAcrossQueriesAndKBs(t *testing.T) {
	q, i, p := refinementFixture()
	out, e := retrievalplan.BuildRefinement(q, i, retrievalplan.Limits{MaxCalls: 10, MaxParallel: 2}, p)
	if e != nil {
		t.Fatal(e)
	}
	total, calls := 0, 0
	for _, r := range out.Routes {
		quota := 0
		for _, c := range r.Calls {
			if c.Call.Limit < 1 || c.QueryText != p.Queries[c.QueryIndex].Text {
				t.Fatal("invalid variant call")
			}
			quota += c.Call.Limit
			calls++
		}
		if quota != r.Limit {
			t.Fatal("route quota expanded")
		}
		total += quota
	}
	if total != 10 || calls != 10 {
		t.Fatal(total, calls)
	}
	p.Queries[0].Text = "changed"
	i["vector"][0].BackendEpochID = "changed"
	if out.Routes[0].Calls[0].QueryText == "changed" || out.Routes[1].Calls[0].Call.Target.BackendEpochID == "changed" {
		t.Fatal("plan aliases input")
	}
}

func TestRefinementRejectsUncoveredRequiredUnknownDuplicateAndTooManyQueries(t *testing.T) {
	for _, change := range []func(*contracts.QueryPlan){func(p *contracts.QueryPlan) {
		for i := range p.Queries {
			p.Queries[i].RouteIDs = []contracts.ID{"mixed"}
		}
	}, func(p *contracts.QueryPlan) { p.Queries[0].RouteIDs = []contracts.ID{"unknown"} }, func(p *contracts.QueryPlan) { p.Queries[0].RouteIDs = []contracts.ID{"vector", "vector"} }, func(p *contracts.QueryPlan) { p.Queries[1] = p.Queries[0] }, func(p *contracts.QueryPlan) { p.Queries = append(p.Queries, p.Queries[0]) }} {
		q, i, p := refinementFixture()
		change(&p)
		if _, e := retrievalplan.BuildRefinement(q, i, retrievalplan.Limits{MaxCalls: 20, MaxParallel: 2}, p); e != retrievalplan.ErrInvalid {
			t.Fatal("invalid refinement accepted", e)
		}
	}
}

func TestRefinementRejectsInsufficientKBQuotaAndTotalFanout(t *testing.T) {
	q, i, p := refinementFixture()
	q.Routes[0].CandidateLimit = 4
	if out, e := retrievalplan.BuildRefinement(q, i, retrievalplan.Limits{MaxCalls: 20, MaxParallel: 2}, p); e != retrievalplan.ErrInvalid || len(out.Routes) != 0 {
		t.Fatal("KB silently dropped")
	}
	q, i, p = refinementFixture()
	if _, e := retrievalplan.BuildRefinement(q, i, retrievalplan.Limits{MaxCalls: 9, MaxParallel: 2}, p); e != retrievalplan.ErrInvalid {
		t.Fatal("variant fanout ignored")
	}
}

func TestRefinementDisabledPolicyAndEmptyQueryRejected(t *testing.T) {
	q, i, p := refinementFixture()
	q.Reflection.Enabled = false
	if _, e := retrievalplan.BuildRefinement(q, i, limits, p); e != retrievalplan.ErrInvalid {
		t.Fatal("disabled reflection planned")
	}
	q, i, p = refinementFixture()
	p.Queries[0].Text = "　 \n"
	if _, e := retrievalplan.BuildRefinement(q, i, limits, p); e != retrievalplan.ErrInvalid {
		t.Fatal("empty query allowed")
	}
}
