package retrievalplan_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/retrievalplan"
)

func profile() contracts.QueryProfile {
	return contracts.QueryProfile{ID: "query", Version: 2, Digest: contracts.Digest(strings.Repeat("a", 64)), Policy: contracts.QueryPolicy{CandidateLimit: 7}, Routes: []contracts.RetrievalRoute{
		{ID: "vector", CandidateLimit: 4, Required: true, Binding: contracts.ModuleBinding{InstanceID: "vec", Capability: contracts.Retrieval}},
		{ID: "mixed", CandidateLimit: 3, Binding: contracts.ModuleBinding{InstanceID: "mix", Capability: contracts.CompositeRetrieval}},
	}}
}
func inventory() map[contracts.ID][]retrievalplan.Target {
	return map[contracts.ID][]retrievalplan.Target{"vector": {{KnowledgeBaseID: "kb-b", BackendEpochID: "eb"}, {KnowledgeBaseID: "kb-a", BackendEpochID: "ea"}, {KnowledgeBaseID: "kb-c", BackendEpochID: "ec"}}, "mixed": {{KnowledgeBaseID: "kb-b", BackendEpochID: "eb"}, {KnowledgeBaseID: "kb-a", BackendEpochID: "ea"}}}
}

var limits = retrievalplan.Limits{MaxCalls: 8, MaxParallel: 2}

func TestQuotaIsGlobalAndPermutationIndependent(t *testing.T) {
	q, inv := profile(), inventory()
	plan, err := retrievalplan.Build(q, inv, limits)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, r := range plan.Routes {
		n := 0
		for _, call := range r.Calls {
			if call.Limit < 1 {
				t.Fatal("target starved")
			}
			n += call.Limit
		}
		if n != r.Limit {
			t.Fatal("route budget mismatch")
		}
		total += n
	}
	if total != 7 || len(plan.Routes) != 2 || plan.Routes[1].Calls[0].Limit != 2 {
		t.Fatal("incorrect quota", plan)
	}
	q.Routes[0], q.Routes[1] = q.Routes[1], q.Routes[0]
	inv["vector"][0], inv["vector"][2] = inv["vector"][2], inv["vector"][0]
	again, err := retrievalplan.Build(q, inv, limits)
	if err != nil || !reflect.DeepEqual(plan, again) {
		t.Fatal("arrival order changed plan", err)
	}
}
func TestImpossibleAdmissionDoesNotSkipTargets(t *testing.T) {
	inv := inventory()
	inv["mixed"] = append(inv["mixed"], retrievalplan.Target{KnowledgeBaseID: "kb-c", BackendEpochID: "ec"}, retrievalplan.Target{KnowledgeBaseID: "kb-d", BackendEpochID: "ed"})
	if p, err := retrievalplan.Build(profile(), inv, limits); err == nil || len(p.Routes) != 0 {
		t.Fatal("silently dropped KB")
	}
	if _, err := retrievalplan.Build(profile(), inventory(), retrievalplan.Limits{MaxCalls: 4, MaxParallel: 2}); err == nil {
		t.Fatal("fanout bound ignored")
	}
	q := profile()
	q.Policy.CandidateLimit = 6
	if _, err := retrievalplan.Build(q, inventory(), limits); err == nil {
		t.Fatal("route budget exceeds global")
	}
}
func TestMissingUnknownAndDuplicateTargetsRejected(t *testing.T) {
	for _, change := range []func(map[contracts.ID][]retrievalplan.Target){
		func(i map[contracts.ID][]retrievalplan.Target) { delete(i, "mixed") },
		func(i map[contracts.ID][]retrievalplan.Target) { i["unknown"] = nil },
		func(i map[contracts.ID][]retrievalplan.Target) { i["vector"][1] = i["vector"][0] },
		func(i map[contracts.ID][]retrievalplan.Target) { i["vector"][0].BackendEpochID = "" },
	} {
		inv := inventory()
		change(inv)
		if _, err := retrievalplan.Build(profile(), inv, limits); err == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
}
func TestExplicitEmptyInventoryAndInputCopies(t *testing.T) {
	q, inv := profile(), inventory()
	inv["vector"] = nil
	p, err := retrievalplan.Build(q, inv, limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Routes[1].Calls) != 0 || !p.Routes[1].Required {
		t.Fatal("empty required route disappeared")
	}
	inv["mixed"][0].BackendEpochID = "changed"
	q.Routes[1].CandidateLimit = 100
	if p.Routes[0].Calls[1].Target.BackendEpochID != "eb" || p.Routes[0].Limit != 3 {
		t.Fatal("plan aliases caller data")
	}
}
func TestInvalidProfileAndHostLimitsRejected(t *testing.T) {
	q := profile()
	q.Routes[1].ID = q.Routes[0].ID
	if _, err := retrievalplan.Build(q, inventory(), limits); err == nil {
		t.Fatal("duplicate route")
	}
	q = profile()
	q.Digest = contracts.Digest(strings.Repeat("z", 64))
	if _, err := retrievalplan.Build(q, inventory(), limits); err == nil {
		t.Fatal("invalid digest")
	}
	for _, l := range []retrievalplan.Limits{{0, 1}, {257, 1}, {8, 0}, {8, 33}, {1, 2}} {
		if _, err := retrievalplan.Build(profile(), inventory(), l); err == nil {
			t.Fatal("invalid limits", l)
		}
	}
}
