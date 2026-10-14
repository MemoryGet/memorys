package retrievalplan

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"sort"
)

type RefinementSearchFunc func(context.Context, RefinementCall) ([]contracts.Candidate, error)
type RefinementFailure struct {
	QueryIndex int
	Target     Target
}
type RefinementDegradation struct {
	RouteID     contracts.ID
	FailedCalls []RefinementFailure
}
type RefinementExecution struct {
	Routes   []RefinementMerge
	Degraded []RefinementDegradation
}

// ExecuteRefinement runs ONE bounded round. The application callback prepares
// real scope/publication/vector bindings; this does not call a Reflector or
// authorize data. It shares the base retrieval worker and byte/failure policy.
func ExecuteRefinement(ctx context.Context, input RefinementPlan, search RefinementSearchFunc) (RefinementExecution, error) {
	if ctx == nil || search == nil {
		return RefinementExecution{}, ErrInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return RefinementExecution{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return RefinementExecution{}, err
	}
	if input.MaxPlanQueries < 1 || input.MaxPlanQueries > 4 || input.MaxCalls < 1 || input.MaxCalls > 256 || input.MaxParallel < 1 || input.MaxParallel > 32 || input.MaxParallel > input.MaxCalls || input.CandidateLimit < 1 || input.CandidateLimit > 1000 || len(input.Routes) < 1 || len(input.Routes) > 32 {
		return RefinementExecution{}, ErrInvalid
	}
	routes := append([]RefinementRoute(nil), input.Routes...)
	sort.Slice(routes, func(i, j int) bool { return routes[i].ID < routes[j].ID })
	var queue []dispatchJob
	quota := 0
	queryTexts := map[int]string{}
	for i := range routes {
		routes[i].Calls = append([]RefinementCall(nil), routes[i].Calls...)
		if i > 0 && routes[i].ID == routes[i-1].ID {
			return RefinementExecution{}, ErrInvalid
		}
		quota += routes[i].Limit
		if quota > input.CandidateLimit {
			return RefinementExecution{}, ErrInvalid
		}
		var empty []RefinementResult
		for _, call := range routes[i].Calls {
			if call.QueryIndex < 0 || call.QueryIndex >= input.MaxPlanQueries {
				return RefinementExecution{}, ErrInvalid
			}
			if old, exists := queryTexts[call.QueryIndex]; exists && old != call.QueryText {
				return RefinementExecution{}, ErrInvalid
			}
			queryTexts[call.QueryIndex] = call.QueryText
			empty = append(empty, RefinementResult{call.QueryIndex, PartitionResult{Target: call.Call.Target}})
			queue = append(queue, dispatchJob{route: i, call: call.Call, queryIndex: call.QueryIndex, queryText: call.QueryText})
		}
		if len(queue) > input.MaxCalls {
			return RefinementExecution{}, ErrInvalid
		}
		if _, err := MergeRefinement(ctx, routes[i], empty); err != nil {
			return RefinementExecution{}, err
		}
	}
	work, err := dispatch(ctx, input.MaxParallel, len(routes), queue, func(c context.Context, j dispatchJob) ([]contracts.Candidate, error) {
		return search(c, RefinementCall{j.queryIndex, j.queryText, j.call})
	})
	if err != nil {
		return RefinementExecution{}, err
	}
	out := RefinementExecution{}
	for i, route := range routes {
		if len(work.failures[i]) > 0 {
			var failed []RefinementFailure
			for _, j := range work.failures[i] {
				failed = append(failed, RefinementFailure{j.queryIndex, j.call.Target})
			}
			sort.Slice(failed, func(a, b int) bool {
				if failed[a].QueryIndex != failed[b].QueryIndex {
					return failed[a].QueryIndex < failed[b].QueryIndex
				}
				return failed[a].Target.KnowledgeBaseID < failed[b].Target.KnowledgeBaseID
			})
			out.Degraded = append(out.Degraded, RefinementDegradation{route.ID, failed})
			continue
		}
		var results []RefinementResult
		for _, s := range work.results[i] {
			results = append(results, RefinementResult{s.job.queryIndex, s.result})
		}
		merged, err := MergeRefinement(ctx, route, results)
		if err != nil {
			return RefinementExecution{}, err
		}
		out.Routes = append(out.Routes, merged)
	}
	if err := ctx.Err(); err != nil {
		return RefinementExecution{}, err
	}
	return out, nil
}
