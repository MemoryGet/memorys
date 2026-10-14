package retrievalplan

import (
	"context"
	"errors"
	"sort"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// Only trusted adapters may classify a transport failure as unavailable.
// Authorization, source and contract failures must never use this sentinel.
var ErrUnavailable = errors.New("retrieval transport unavailable")

// A joined/multi-cause error cannot establish that the failure was exclusively
// transport-related. Only a bounded single wrapping chain is accepted.
func transportUnavailable(err error) bool {
	for depth := 0; depth < 16 && err != nil; depth++ {
		if err == ErrUnavailable {
			return true
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return false
		}
		wrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = wrapper.Unwrap()
	}
	return false
}

type SearchFunc func(context.Context, Call) ([]contracts.Candidate, error)
type Degradation struct {
	RouteID       contracts.ID
	FailedTargets []Target
}
type Execution struct {
	Routes   []MergeResult
	Degraded []Degradation
}

// Execute uses a fixed worker pool. The trusted callback must obey cancellation
// and transfer ownership of returned data. This helper cannot kill a callback
// that ignores context; it joins every started worker before returning.
func Execute(ctx context.Context, input Plan, search SearchFunc) (Execution, error) {
	if ctx == nil || search == nil {
		return Execution{}, ErrInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return Execution{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Execution{}, err
	}
	if input.MaxCalls < 1 || input.MaxCalls > 256 || input.MaxParallel < 1 || input.MaxParallel > 32 || input.MaxParallel > input.MaxCalls || input.CandidateLimit < 1 || input.CandidateLimit > 1000 || len(input.Routes) < 1 || len(input.Routes) > 32 {
		return Execution{}, ErrInvalid
	}
	routes := append([]Route(nil), input.Routes...)
	sort.Slice(routes, func(i, j int) bool { return routes[i].ID < routes[j].ID })
	var queue []dispatchJob
	allocated := 0
	for i := range routes {
		routes[i].Calls = append([]Call(nil), routes[i].Calls...)
		if i > 0 && routes[i].ID == routes[i-1].ID {
			return Execution{}, ErrInvalid
		}
		allocated += routes[i].Limit
		if allocated > input.CandidateLimit || len(routes[i].Calls) > 256 {
			return Execution{}, ErrInvalid
		}
		var empty []PartitionResult
		for _, call := range routes[i].Calls {
			empty = append(empty, PartitionResult{Target: call.Target})
			queue = append(queue, dispatchJob{route: i, call: call})
		}
		if _, err := Merge(ctx, routes[i], empty); err != nil {
			return Execution{}, err
		}
		if len(queue) > input.MaxCalls {
			return Execution{}, ErrInvalid
		}
	}
	workResult, err := dispatch(ctx, input.MaxParallel, len(routes), queue, func(c context.Context, job dispatchJob) ([]contracts.Candidate, error) { return search(c, job.call) })
	if err != nil {
		return Execution{}, err
	}
	results := make([][]PartitionResult, len(routes))
	failures := make([][]Target, len(routes))
	for i := range routes {
		for _, success := range workResult.results[i] {
			results[i] = append(results[i], success.result)
		}
		for _, failure := range workResult.failures[i] {
			failures[i] = append(failures[i], failure.call.Target)
		}
	}
	out := Execution{}
	for i, route := range routes {
		if len(failures[i]) > 0 {
			sort.Slice(failures[i], func(a, b int) bool { return failures[i][a].KnowledgeBaseID < failures[i][b].KnowledgeBaseID })
			out.Degraded = append(out.Degraded, Degradation{RouteID: route.ID, FailedTargets: failures[i]})
			continue
		}
		merged, err := Merge(ctx, route, results[i])
		if err != nil {
			return Execution{}, err
		}
		out.Routes = append(out.Routes, merged)
	}
	if err := ctx.Err(); err != nil {
		return Execution{}, err
	}
	return out, nil
}
