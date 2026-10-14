package fusion_test

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/fusion"
	"strings"
	"testing"
)

func profiles() (contracts.PipelineProfile, contracts.QueryProfile) {
	q := contracts.QueryProfile{ID: "query", Version: 3, Digest: contracts.Digest(strings.Repeat("b", 64)), Policy: contracts.QueryPolicy{FusionMethod: "rrf", RankConstant: 60, CandidateLimit: 4}, Routes: []contracts.RetrievalRoute{
		{ID: "a", CandidateLimit: 2, Required: true, Binding: contracts.ModuleBinding{InstanceID: "a-backend", Capability: contracts.Retrieval}},
		{ID: "b", CandidateLimit: 2, Required: false, Binding: contracts.ModuleBinding{InstanceID: "b-backend", Capability: contracts.CompositeRetrieval}}}}
	p := contracts.PipelineProfile{ID: "pipeline", Version: 2, QueryProfile: contracts.ProfileReference{ID: q.ID, Version: q.Version}, Digest: contracts.Digest(strings.Repeat("a", 64))}
	return p, q
}
func configuredRequest(lists ...contracts.RankedList) contracts.FuseRequest {
	r := request(lists...)
	r.Limit = 4
	return r
}
func routeA() contracts.RankedList {
	return contracts.RankedList{RouteID: "a", Candidates: []contracts.Candidate{candidate("a", "x", 1, 0)}}
}

func TestConfiguredRoutesBindProfileAndAllowMissingOptional(t *testing.T) {
	p, q := profiles()
	f, err := fusion.NewRRFForProfile(p, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Fuse(context.Background(), configuredRequest(routeA())); err != nil {
		t.Fatal("optional route absence rejected", err)
	}
	empty := contracts.RankedList{RouteID: "a"}
	out, err := f.Fuse(context.Background(), configuredRequest(empty))
	if err != nil || len(out) != 0 {
		t.Fatal("valid required route empty result rejected", err)
	}
	if _, err := f.Fuse(context.Background(), configuredRequest()); err == nil {
		t.Fatal("required route missing")
	}
}

func TestInjectedRouteBackendAndProfilePolicyOverridesRejected(t *testing.T) {
	p, q := profiles()
	f, err := fusion.NewRRFForProfile(p, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*contracts.FuseRequest){
		func(r *contracts.FuseRequest) {
			r.Lists = append(r.Lists, contracts.RankedList{RouteID: "injected", Candidates: []contracts.Candidate{candidate("injected", "x", 1, 0)}})
		},
		func(r *contracts.FuseRequest) { r.Lists[0].Candidates[0].RetrieverID = "imposter" },
		func(r *contracts.FuseRequest) { r.Request.ProfileDigest = contracts.Digest(strings.Repeat("c", 64)) },
		func(r *contracts.FuseRequest) { r.RankConstant = 1 }, func(r *contracts.FuseRequest) { r.Limit = 5 }} {
		r := configuredRequest(routeA())
		mutate(&r)
		if _, err := f.Fuse(context.Background(), r); err == nil {
			t.Fatal("unapproved override accepted")
		}
	}
}

func TestProfileBudgetAndConstructorConsistency(t *testing.T) {
	p, q := profiles()
	f, err := fusion.NewRRFForProfile(p, q)
	if err != nil {
		t.Fatal(err)
	}
	a := routeA()
	a.Candidates = append(a.Candidates, candidate("a", "y", 2, 0), candidate("a", "z", 3, 0))
	if _, err := f.Fuse(context.Background(), configuredRequest(a)); err == nil {
		t.Fatal("route budget exceeded")
	}
	a = routeA()
	a.Candidates = append(a.Candidates, candidate("a", "y", 2, 0))
	if _, err := f.Fuse(context.Background(), configuredRequest(a, a, a)); err == nil {
		t.Fatal("repeated lists bypassed global input budget")
	}
	q.Routes[1].CandidateLimit = 3
	if _, err := fusion.NewRRFForProfile(p, q); err == nil {
		t.Fatal("route allocation exceeds global budget")
	}
	p, q = profiles()
	p.QueryProfile.Version++
	if _, err := fusion.NewRRFForProfile(p, q); err == nil {
		t.Fatal("wrong query version bound")
	}
}

func TestCapturedProfileCannotBeModifiedByCaller(t *testing.T) {
	p, q := profiles()
	f, err := fusion.NewRRFForProfile(p, q)
	if err != nil {
		t.Fatal(err)
	}
	before := f.Identity()
	p.Digest = "changed"
	q.Routes[0].Binding.InstanceID = "changed"
	q.Routes[0].Required = false
	q.Policy.RankConstant = 1
	if _, err := f.Fuse(context.Background(), configuredRequest(routeA())); err != nil || f.Identity() != before {
		t.Fatal("mutable input changed captured policy", err)
	}
	if _, err := f.Fuse(context.Background(), configuredRequest()); err == nil {
		t.Fatal("required policy mutated")
	}
}
