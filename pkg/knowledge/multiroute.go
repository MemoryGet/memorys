package knowledge

import (
	"context"
	"encoding/json"
	"math"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/fusion"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type multiRoutePlans struct {
	profile       configprofile.Snapshot
	source        PlanSource
	fuser         contracts.Fuser
	requiresGuard bool
}

func newMultiRoutePlans(p configprofile.Snapshot, source PlanSource, fuser contracts.Fuser) (PlanSource, error) {
	raw, err := configprofile.Encode(p)
	if err != nil {
		return nil, ErrSearch
	}
	owned, err := configprofile.Decode(raw)
	if err != nil {
		return nil, ErrSearch
	}
	configured, err := fusion.NewRRFForProfile(owned.Pipeline, owned.Query)
	if err != nil {
		return nil, ErrSearch
	}
	requiresGuard := !missing(fuser)
	if missing(fuser) {
		fuser = configured
	}
	return &multiRoutePlans{profile: owned, source: source, fuser: fuser, requiresGuard: requiresGuard}, nil
}
func (p *multiRoutePlans) Prepare(ctx context.Context, who contracts.Principal, base contracts.RetrieveRequest) (SearchPlan, error) {
	combined := SearchPlan{Request: base}
	calls := []SearchPlan{}
	facts := map[contracts.ID]contracts.PublishedFact{}
	for _, route := range p.profile.Query.Routes {
		request, err := copyValue(base)
		if err != nil {
			return SearchPlan{}, ErrSearch
		}
		request.RouteID = route.ID
		request.Channels = append([]contracts.Channel(nil), route.Channels...)
		request.Limit = route.CandidateLimit
		saved, err := copyValue(request)
		if err != nil {
			return SearchPlan{}, ErrSearch
		}
		actor, err := copyValue(who)
		if err != nil {
			return SearchPlan{}, ErrSearch
		}
		part, err := p.source.Prepare(ctx, actor, request)
		// Until degraded-state transport exists, every configured route must work.
		if err != nil || missing(part.Retriever) || ctx.Err() != nil {
			return SearchPlan{}, ErrSearch
		}
		fixed, err := copyValue(part.Request)
		if err != nil || !reflect.DeepEqual(fixed.Request, saved.Request) || !reflect.DeepEqual(fixed.Scope, saved.Scope) || !reflect.DeepEqual(fixed.Query, saved.Query) || fixed.Limit != saved.Limit || fixed.RouteID != saved.RouteID || !reflect.DeepEqual(fixed.Channels, saved.Channels) {
			return SearchPlan{}, ErrSearch
		}
		expected, err := copyValue(part.Expectation)
		if err != nil || expected.ScopeVersion != base.Scope.PolicyVersion || expected.StoreEpochID == "" {
			return SearchPlan{}, ErrSearch
		}
		if len(calls) == 0 {
			combined.Expectation = queryread.CurrentExpectation{StoreEpochID: expected.StoreEpochID, ScopeVersion: expected.ScopeVersion}
			combined.Reranker = part.Reranker
		} else if combined.Expectation.StoreEpochID != expected.StoreEpochID {
			return SearchPlan{}, ErrSearch
		}
		for _, fact := range expected.Published {
			if old, ok := facts[fact.DocumentID]; ok {
				if !reflect.DeepEqual(old, fact) {
					return SearchPlan{}, ErrSearch
				}
			} else {
				facts[fact.DocumentID] = fact
				combined.Expectation.Published = append(combined.Expectation.Published, fact)
			}
		}
		if len(facts) > 1000 {
			return SearchPlan{}, ErrSearch
		}
		part.Request = fixed
		part.Expectation = expected
		calls = append(calls, part)
	}
	combined.Retriever = &multiRouteRetriever{profile: p.profile, base: base, calls: calls, fuser: p.fuser, requiresGuard: p.requiresGuard}
	return combined, nil
}

type multiRouteRetriever struct {
	profile       configprofile.Snapshot
	base          contracts.RetrieveRequest
	calls         []SearchPlan
	fuser         contracts.Fuser
	requiresGuard bool
	guard         func(context.Context, []contracts.RankedList) error
}

func (p *multiRouteRetriever) Search(ctx context.Context, q contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	if ctx == nil || ctx.Err() != nil || !reflect.DeepEqual(q, p.base) {
		return nil, ErrSearch
	}
	lists := []contracts.RankedList{}
	size := 0
	for i, call := range p.calls {
		request, err := copyValue(call.Request)
		if err != nil {
			return nil, ErrSearch
		}
		results, err := call.Retriever.Search(ctx, request)
		if err != nil || ctx.Err() != nil || len(results) > request.Limit {
			return nil, ErrSearch
		}
		results, err = copyValue(results)
		if err != nil {
			return nil, ErrSearch
		}
		route := p.profile.Query.Routes[i]
		for rank, c := range results {
			size += len(c.Text)
			if c.Rank != rank+1 || c.RouteID != route.ID || c.RetrieverID != route.Binding.InstanceID || !validRawCandidate(c) || size > 4<<20 {
				return nil, ErrSearch
			}
		}
		lists = append(lists, contracts.RankedList{RouteID: route.ID, Candidates: results})
	}
	if p.requiresGuard {
		if p.guard == nil {
			return nil, ErrSearch
		}
		checked, err := copyValue(lists)
		if err != nil || p.guard(ctx, checked) != nil || ctx.Err() != nil {
			return nil, ErrSearch
		}
	}
	input, err := copyValue(contracts.FuseRequest{Request: q.Request, Lists: lists, RankConstant: p.profile.Query.Policy.RankConstant, Limit: q.Limit})
	if err != nil {
		return nil, ErrSearch
	}
	results, err := p.fuser.Fuse(ctx, input)
	if err != nil || ctx.Err() != nil || len(results) > q.Limit {
		return nil, ErrSearch
	}
	results, err = copyValue(results)
	if err != nil {
		return nil, ErrSearch
	}
	// Replaceable fusers may rank known sources, never invent a citation/body.
	seen := map[string]bool{}
	for i, c := range results {
		if c.Rank != i+1 {
			return nil, ErrSearch
		}
		found := false
		for _, list := range lists {
			for _, original := range list.Candidates {
				if c.ChunkID == original.ChunkID && c.KnowledgeBaseID == original.KnowledgeBaseID && reflect.DeepEqual(c.Citation, original.Citation) && c.Text == original.Text {
					found = true
				}
			}
		}
		key := sourceIdentity(c.KnowledgeBaseID, c.ChunkID, c.Citation)

		if !found || seen[key] || !fusionLineage(c, lists) {
			return nil, ErrSearch
		}
		seen[key] = true
	}
	return results, nil
}

func sourceIdentity(kb, chunk contracts.ID, citation contracts.Citation) string {
	raw, _ := json.Marshal(struct {
		KB, Chunk contracts.ID
		Citation  contracts.Citation
	}{kb, chunk, citation})
	return string(raw)
}

func fusionLineage(c contracts.Candidate, lists []contracts.RankedList) bool {
	known := map[contracts.ID]bool{}
	representative := false
	for _, list := range lists {
		for _, original := range list.Candidates {
			if sourceIdentity(c.KnowledgeBaseID, c.ChunkID, c.Citation) == sourceIdentity(original.KnowledgeBaseID, original.ChunkID, original.Citation) && c.Text == original.Text {
				for _, g := range original.GenerationIDs {
					known[g] = true
				}
				if c.RouteID == original.RouteID && c.RetrieverID == original.RetrieverID {
					representative = true
				}
			}
		}
	}
	if !representative || len(c.GenerationIDs) == 0 || len(c.FusionOrigins) == 0 {
		return false
	}
	generations := map[contracts.ID]bool{}
	for _, g := range c.GenerationIDs {
		if !known[g] || generations[g] {
			return false
		}
		generations[g] = true
	}
	seen := map[string]bool{}
	for _, origin := range c.FusionOrigins {
		key, _ := json.Marshal(struct{ Route, Retriever contracts.ID }{origin.RouteID, origin.RetrieverID})
		if seen[string(key)] {
			return false
		}
		seen[string(key)] = true
		matched := false
		for _, list := range lists {
			for _, original := range list.Candidates {
				if origin.RouteID != list.RouteID || origin.RetrieverID != original.RetrieverID || origin.Rank != original.Rank || sourceIdentity(c.KnowledgeBaseID, c.ChunkID, c.Citation) != sourceIdentity(original.KnowledgeBaseID, original.ChunkID, original.Citation) || c.Text != original.Text {
					continue
				}
				allowed := map[contracts.ID]bool{}
				for _, g := range original.GenerationIDs {
					allowed[g] = true
				}
				valid := len(origin.GenerationIDs) > 0
				used := map[contracts.ID]bool{}
				for _, g := range origin.GenerationIDs {
					if !allowed[g] || !generations[g] || used[g] {
						valid = false
					}
					used[g] = true
				}
				matched = matched || valid
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func validRawCandidate(c contracts.Candidate) bool {
	if !id(c.ChunkID) || !id(c.KnowledgeBaseID) || !id(c.Citation.DocumentID) || !id(c.Citation.RevisionID) || !id(c.Citation.BlockID) || c.Text == "" || math.IsNaN(c.Score) || math.IsInf(c.Score, 0) || len(c.GenerationIDs) == 0 || len(c.GenerationIDs) > 1000 || len(c.FusionOrigins) != 0 {
		return false
	}
	seen := map[contracts.ID]bool{}
	for _, g := range c.GenerationIDs {
		if !id(g) || seen[g] {
			return false
		}
		seen[g] = true
	}
	return true
}
