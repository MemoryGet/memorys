package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/fusion"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type planFunc func(context.Context, contracts.Principal, contracts.RetrieveRequest) (SearchPlan, error)

func (f planFunc) Prepare(c context.Context, p contracts.Principal, q contracts.RetrieveRequest) (SearchPlan, error) {
	return f(c, p, q)
}

type retrievalFunc func(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error)

func (f retrievalFunc) Search(c context.Context, q contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	return f(c, q)
}

type fuseFunc func(context.Context, contracts.FuseRequest) ([]contracts.Candidate, error)

func (f fuseFunc) Fuse(c context.Context, q contracts.FuseRequest) ([]contracts.Candidate, error) {
	return f(c, q)
}
func multiFixture(t *testing.T) (configprofile.Snapshot, contracts.RetrieveRequest, PlanSource, map[contracts.ID]int) {
	t.Helper()
	a := contracts.ModuleBinding{InstanceID: "reader-a", Capability: contracts.Retrieval, ContractVersion: "1.0"}
	b := a
	b.InstanceID = "reader-b"
	p, err := configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1", Pipeline: contracts.PipelineProfile{ID: "pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "query", Version: 1}}, Index: contracts.IndexProfile{ID: "index", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: {InstanceID: "writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}}}, Query: contracts.QueryProfile{ID: "query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: a}, Routes: []contracts.RetrievalRoute{{ID: "a", Binding: a, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 5, Required: true}, {ID: "b", Binding: b, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 5, Required: true}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 10, AnswerEvidenceLimit: 5, FusionMethod: "rrf", RankConstant: 60, RetrievalTimeoutMS: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(time.Minute)
	base := contracts.RetrieveRequest{Request: contracts.RequestContext{RequestID: "request", ProfileDigest: p.Pipeline.Digest, Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "user", Action: "knowledge.search", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: deadline}, Query: contracts.Query{Text: "事实"}, RouteID: "a", Channels: []contracts.Channel{contracts.KeywordChannel}, Limit: 1}
	calls := map[contracts.ID]int{}
	source := planFunc(func(_ context.Context, _ contracts.Principal, q contracts.RetrieveRequest) (SearchPlan, error) {
		if q.Limit != 5 {
			t.Fatal("route allocation ignored", q.Limit)
		}
		return SearchPlan{Request: q, Expectation: queryread.CurrentExpectation{StoreEpochID: "store", ScopeVersion: 1, Published: []contracts.PublishedFact{{DocumentID: "doc", RevisionID: "rev", SpaceID: "space", KnowledgeBaseID: "kb", RowVersion: 1}}}, Retriever: retrievalFunc(func(_ context.Context, r contracts.RetrieveRequest) ([]contracts.Candidate, error) {
			calls[r.RouteID]++
			return []contracts.Candidate{{Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}, ChunkID: "chunk", KnowledgeBaseID: "kb", RouteID: r.RouteID, RetrieverID: contracts.ID("reader-" + string(r.RouteID)), Rank: 1, Text: "事实", GenerationIDs: []contracts.ID{"generation"}}}, nil
		})}, nil
	})
	return p, base, source, calls
}
func TestMultiRouteUsesAllBudgetsAndPreservesFusionOrigins(t *testing.T) {
	p, q, source, calls := multiFixture(t)
	planner, err := newMultiRoutePlans(p, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Prepare(context.Background(), contracts.Principal{}, q)
	if err != nil {
		t.Fatal(err)
	}
	got, err := plan.Retriever.Search(context.Background(), q)
	if err != nil || len(got) != 1 || len(got[0].FusionOrigins) != 2 || calls["a"] != 1 || calls["b"] != 1 || len(plan.Expectation.Published) != 1 {
		t.Fatal("multi-route coverage lost", err, calls, got)
	}
}
func TestMultiRouteRejectsFailureAndMixedStoreBeforeRetrieval(t *testing.T) {
	for _, mode := range []string{"failure", "store"} {
		p, q, source, calls := multiFixture(t)
		wrapped := planFunc(func(c context.Context, who contracts.Principal, r contracts.RetrieveRequest) (SearchPlan, error) {
			if r.RouteID == "b" && mode == "failure" {
				return SearchPlan{}, errors.New("route unavailable")
			}
			plan, err := source.Prepare(c, who, r)
			if r.RouteID == "b" {
				plan.Expectation.StoreEpochID = "other-store"
			}
			return plan, err
		})
		planner, err := newMultiRoutePlans(p, wrapped, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := planner.Prepare(context.Background(), contracts.Principal{}, q); err == nil || len(calls) != 0 {
			t.Fatal("partial or mixed-store query accepted")
		}
	}
}
func TestReplaceableFuserCannotInventSourcesOrGenerations(t *testing.T) {
	for _, mode := range []string{"source", "generation", "origin"} {
		p, q, source, _ := multiFixture(t)
		invoked := false
		custom := fuseFunc(func(c context.Context, r contracts.FuseRequest) ([]contracts.Candidate, error) {
			invoked = true
			out, err := (fusion.RRF{}).Fuse(c, r)
			if err != nil {
				return nil, err
			}
			switch mode {
			case "source":
				out[0].Text = "编造"
			case "generation":
				out[0].GenerationIDs = []contracts.ID{"other"}
			case "origin":
				out[0].FusionOrigins[0].RetrieverID = "other"
			}
			return out, nil
		})
		planner, err := newMultiRoutePlans(p, source, custom)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := planner.Prepare(context.Background(), contracts.Principal{}, q)
		if err != nil {
			t.Fatal(err)
		}
		plan.Retriever.(*multiRouteRetriever).guard = func(context.Context, []contracts.RankedList) error { return nil }
		if out, err := plan.Retriever.Search(context.Background(), q); err == nil || out != nil {
			t.Fatal("untrusted fusion output escaped", mode)
		}
		if !invoked {
			t.Fatal("fusion validation was not exercised")
		}
	}
}

func TestReplaceableFuserRequiresSuccessfulPreverification(t *testing.T) {
	for _, mode := range []string{"missing", "denied", "valid"} {
		p, q, source, _ := multiFixture(t)
		called := false
		verified := false
		custom := fuseFunc(func(c context.Context, r contracts.FuseRequest) ([]contracts.Candidate, error) {
			called = true
			if !verified {
				t.Fatal("fuser called before verification")
			}
			return (fusion.RRF{}).Fuse(c, r)
		})
		planner, err := newMultiRoutePlans(p, source, custom)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := planner.Prepare(context.Background(), contracts.Principal{}, q)
		if err != nil {
			t.Fatal(err)
		}
		if mode != "missing" {
			plan.Retriever.(*multiRouteRetriever).guard = func(_ context.Context, lists []contracts.RankedList) error {
				if mode == "denied" {
					return errors.New("revoked")
				}
				verified = true
				lists[0].Candidates[0].Text = "mutated guard argument"
				return nil
			}
		}
		out, err := plan.Retriever.Search(context.Background(), q)
		if mode == "valid" {
			if err != nil || !called || len(out) != 1 || out[0].Text != "事实" {
				t.Fatal("guard or input ownership failed", err)
			}
		} else if err == nil || out != nil || called {
			t.Fatal("unverified candidate reached fuser")
		}
	}
}

func TestRouteCandidateValidationDoesNotDependOnFuser(t *testing.T) {
	for _, mode := range []string{"chunk", "generation", "prior-fusion"} {
		p, q, source, _ := multiFixture(t)
		wrapped := planFunc(func(c context.Context, who contracts.Principal, r contracts.RetrieveRequest) (SearchPlan, error) {
			plan, err := source.Prepare(c, who, r)
			original := plan.Retriever
			plan.Retriever = retrievalFunc(func(c context.Context, r contracts.RetrieveRequest) ([]contracts.Candidate, error) {
				out, err := original.Search(c, r)
				switch mode {
				case "chunk":
					out[0].ChunkID = ""
				case "generation":
					out[0].GenerationIDs = []contracts.ID{"generation", "generation"}
				case "prior-fusion":
					out[0].FusionOrigins = []contracts.FusionOrigin{{RouteID: "untrusted"}}
				}
				return out, err
			})
			return plan, err
		})
		called := false
		custom := fuseFunc(func(context.Context, contracts.FuseRequest) ([]contracts.Candidate, error) {
			called = true
			return nil, nil
		})
		planner, err := newMultiRoutePlans(p, wrapped, custom)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := planner.Prepare(context.Background(), contracts.Principal{}, q)
		if err != nil {
			t.Fatal(err)
		}
		plan.Retriever.(*multiRouteRetriever).guard = func(context.Context, []contracts.RankedList) error { return nil }
		if out, err := plan.Retriever.Search(context.Background(), q); err == nil || out != nil || called {
			t.Fatal("invalid raw candidate reached replaceable fuser", mode)
		}
	}
}
