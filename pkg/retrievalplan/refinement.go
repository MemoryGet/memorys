package retrievalplan

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type RefinementCall struct {
	QueryIndex int
	QueryText  string
	Call       Call
}
type RefinementRoute struct {
	ID       contracts.ID
	Limit    int
	Required bool
	Calls    []RefinementCall
}
type RefinementPlan struct {
	Query                                                 contracts.ProfileReference
	QueryDigest                                           contracts.Digest
	CandidateLimit, MaxCalls, MaxParallel, MaxPlanQueries int
	Routes                                                []RefinementRoute
}

// BuildRefinement allocates one round's route quotas across selected query
// variants and all eligible KBs. It does not execute, authorize or merge results.
func BuildRefinement(q contracts.QueryProfile, inventory map[contracts.ID][]Target, limits Limits, plan contracts.QueryPlan) (RefinementPlan, error) {
	if !q.Reflection.Enabled || q.Reflection.MaxExtraRounds < 1 || q.Reflection.MaxExtraRounds > 2 || q.Reflection.TotalBudgetMS < 1 || q.Reflection.TotalBudgetMS > 30000 || q.Reflection.MaxPlanQueries < 1 || q.Reflection.MaxPlanQueries > 4 || len(plan.Queries) < 1 || len(plan.Queries) > q.Reflection.MaxPlanQueries {
		return RefinementPlan{}, ErrInvalid
	}
	base, err := Build(q, inventory, limits)
	if err != nil {
		return RefinementPlan{}, err
	}
	known := map[contracts.ID]bool{}
	for _, r := range base.Routes {
		known[r.ID] = true
	}
	selected := map[contracts.ID][]int{}
	queries := make([]contracts.PlannedQuery, len(plan.Queries))
	duplicates := map[string]bool{}
	for i, query := range plan.Queries {
		if !utf8.ValidString(query.Text) || strings.TrimSpace(query.Text) == "" || utf8.RuneCountInString(query.Text) > 16000 || len(query.RouteIDs) < 1 || len(query.RouteIDs) > len(base.Routes) {
			return RefinementPlan{}, ErrInvalid
		}
		queries[i] = contracts.PlannedQuery{Text: query.Text, RouteIDs: append([]contracts.ID(nil), query.RouteIDs...)}
		routes := map[contracts.ID]bool{}
		for _, id := range query.RouteIDs {
			if !known[id] || routes[id] {
				return RefinementPlan{}, ErrInvalid
			}
			routes[id] = true
			selected[id] = append(selected[id], i)
		}
		// Canonicalize the route set for duplicate-plan detection, not the text.
		var ordered []contracts.ID
		for _, r := range base.Routes {
			if routes[r.ID] {
				ordered = append(ordered, r.ID)
			}
		}
		key, _ := json.Marshal(contracts.PlannedQuery{Text: query.Text, RouteIDs: ordered})
		if duplicates[string(key)] {
			return RefinementPlan{}, ErrInvalid
		}
		duplicates[string(key)] = true
	}
	out := RefinementPlan{Query: base.Query, QueryDigest: base.QueryDigest, CandidateLimit: base.CandidateLimit, MaxCalls: base.MaxCalls, MaxParallel: base.MaxParallel, MaxPlanQueries: q.Reflection.MaxPlanQueries}
	count := 0
	for _, route := range base.Routes {
		indexes := selected[route.ID]
		if len(indexes) == 0 {
			if route.Required {
				return RefinementPlan{}, ErrInvalid
			}
			continue
		}
		r := RefinementRoute{ID: route.ID, Limit: route.Limit, Required: route.Required}
		for position, index := range indexes {
			quota := route.Limit / len(indexes)
			if position < route.Limit%len(indexes) {
				quota++
			}
			if quota < 1 || quota < len(route.Calls) {
				return RefinementPlan{}, ErrInvalid
			}
			for k, original := range route.Calls {
				call := original
				call.Limit = quota / len(route.Calls)
				if k < quota%len(route.Calls) {
					call.Limit++
				}
				r.Calls = append(r.Calls, RefinementCall{QueryIndex: index, QueryText: queries[index].Text, Call: call})
				count++
				if count > out.MaxCalls {
					return RefinementPlan{}, ErrInvalid
				}
			}
		}
		out.Routes = append(out.Routes, r)
	}
	return out, nil
}
