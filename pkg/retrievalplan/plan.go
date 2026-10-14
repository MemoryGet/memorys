// Package retrievalplan plans bounded per-KB calls from a separately authorized
// inventory. Planning does not authorize targets or execute/schedule calls.
package retrievalplan

import (
	"errors"
	"sort"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrInvalid = errors.New("invalid retrieval plan")

type Target struct {
	KnowledgeBaseID contracts.ID
	BackendEpochID  contracts.ID
}

// Limits are resolved host admission limits, not a new QueryProfile schema.
type Limits struct{ MaxCalls, MaxParallel int }

type Call struct {
	RouteID, RetrieverID contracts.ID
	Target               Target
	Limit                int
	Required             bool
}

type Route struct {
	ID       contracts.ID
	Limit    int
	Required bool
	Calls    []Call
}

type Plan struct {
	Query          contracts.ProfileReference
	QueryDigest    contracts.Digest
	CandidateLimit int
	MaxCalls       int
	MaxParallel    int
	Routes         []Route
}

func idValid(id contracts.ID) bool { return id != "" && len(id) <= 200 && utf8.ValidString(string(id)) }

// Build allocates each route's existing quota across all eligible KBs. Empty
// inventory means no eligible published data; it must not mean an outage.
// Every eligible target receives at least one slot or the whole plan fails.
func Build(query contracts.QueryProfile, inventory map[contracts.ID][]Target, limits Limits) (Plan, error) {
	if !idValid(query.ID) || query.Version == 0 || len(query.Digest) != 64 || query.Policy.CandidateLimit < 1 || query.Policy.CandidateLimit > 1000 || len(query.Routes) < 1 || len(query.Routes) > 32 || limits.MaxCalls < 1 || limits.MaxCalls > 256 || limits.MaxParallel < 1 || limits.MaxParallel > 32 || limits.MaxParallel > limits.MaxCalls {
		return Plan{}, ErrInvalid
	}
	for _, ch := range query.Digest {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return Plan{}, ErrInvalid
		}
	}
	routes := append([]contracts.RetrievalRoute(nil), query.Routes...)
	seen := map[contracts.ID]bool{}
	allocated, calls := 0, 0
	for _, route := range routes {
		if !idValid(route.ID) || !idValid(route.Binding.InstanceID) || seen[route.ID] || (route.Binding.Capability != contracts.Retrieval && route.Binding.Capability != contracts.CompositeRetrieval) || route.CandidateLimit < 1 || route.CandidateLimit > query.Policy.CandidateLimit {
			return Plan{}, ErrInvalid
		}
		seen[route.ID] = true
		allocated += route.CandidateLimit
		if allocated > query.Policy.CandidateLimit {
			return Plan{}, ErrInvalid
		}
		targets, exists := inventory[route.ID]
		if !exists || len(targets) > route.CandidateLimit || len(targets) > limits.MaxCalls {
			return Plan{}, ErrInvalid
		}
		calls += len(targets)
		if calls > limits.MaxCalls {
			return Plan{}, ErrInvalid
		}
	}
	if len(inventory) != len(seen) {
		return Plan{}, ErrInvalid
	}
	for routeID := range inventory {
		if !seen[routeID] {
			return Plan{}, ErrInvalid
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].ID < routes[j].ID })
	plan := Plan{Query: contracts.ProfileReference{ID: query.ID, Version: query.Version}, QueryDigest: query.Digest, CandidateLimit: query.Policy.CandidateLimit, MaxCalls: limits.MaxCalls, MaxParallel: limits.MaxParallel}
	for _, route := range routes {
		targets := append([]Target(nil), inventory[route.ID]...)
		sort.Slice(targets, func(i, j int) bool { return targets[i].KnowledgeBaseID < targets[j].KnowledgeBaseID })
		r := Route{ID: route.ID, Limit: route.CandidateLimit, Required: route.Required}
		for i, target := range targets {
			if !idValid(target.KnowledgeBaseID) || !idValid(target.BackendEpochID) || (i > 0 && target.KnowledgeBaseID == targets[i-1].KnowledgeBaseID) {
				return Plan{}, ErrInvalid
			}
			quota := route.CandidateLimit / len(targets)
			if i < route.CandidateLimit%len(targets) {
				quota++
			}
			r.Calls = append(r.Calls, Call{RouteID: route.ID, RetrieverID: route.Binding.InstanceID, Target: target, Limit: quota, Required: route.Required})
		}
		plan.Routes = append(plan.Routes, r)
	}
	return plan, nil
}
