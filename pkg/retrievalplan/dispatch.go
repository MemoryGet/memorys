package retrievalplan

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"sync"
)

type dispatchJob struct {
	route      int
	call       Call
	queryIndex int
	queryText  string
}
type dispatchSuccess struct {
	job    dispatchJob
	result PartitionResult
}
type dispatchResult struct {
	results  [][]dispatchSuccess
	failures [][]dispatchJob
}

// Shared retrieval pool only; trusted callbacks must obey cancellation.
func dispatch(ctx context.Context, maxParallel, routeCount int, queue []dispatchJob, search func(context.Context, dispatchJob) ([]contracts.Candidate, error)) (dispatchResult, error) {
	if ctx == nil || search == nil || maxParallel < 1 || maxParallel > 32 || routeCount < 1 || routeCount > 32 || len(queue) > 256 {
		return dispatchResult{}, ErrInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return dispatchResult{}, ErrInvalid
	}
	for _, job := range queue {
		if job.route < 0 || job.route >= routeCount {
			return dispatchResult{}, ErrInvalid
		}
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	next := 0
	var fatal error
	chargedBytes := 0
	results := make([][]dispatchSuccess, routeCount)
	failures := make([][]dispatchJob, routeCount)
	invoke := func(job dispatchJob) (c []contracts.Candidate, err error) {
		defer func() {
			if recover() != nil {
				c = nil
				err = ErrInvalid
			}
		}()
		return search(child, job)
	}
	var workers sync.WaitGroup
	count := maxParallel
	if count > len(queue) {
		count = len(queue)
	}
	for n := 0; n < count; n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				mu.Lock()
				if fatal != nil || child.Err() != nil || next == len(queue) {
					mu.Unlock()
					return
				}
				job := queue[next]
				next++
				if len(failures[job.route]) > 0 {
					mu.Unlock()
					continue
				}
				mu.Unlock()
				// Recheck after dequeue so expired queued calls are not invoked.
				if child.Err() != nil {
					return
				}
				candidates, err := invoke(job)
				cost := 0
				if err == nil {
					// Validate and copy before exposing anything to other stages.
					r := Route{ID: job.call.RouteID, Limit: job.call.Limit, Required: job.call.Required, Calls: []Call{job.call}}
					var checked MergeResult
					checked, err = Merge(child, r, []PartitionResult{{Target: job.call.Target, Candidates: candidates}})
					if err == nil {
						// Reserve up to three extra rank digits per candidate for
						// later global ranking. Degraded work does not refund cost.
						cost = checked.EncodedBytes + 3*len(checked.List.Candidates)
						candidates = checked.List.Candidates
						for i := range candidates {
							candidates[i].Rank = checked.Origins[i].OriginalRank
						}
					}
				}
				mu.Lock()
				if err == nil {
					chargedBytes += cost
					if chargedBytes > MaxCandidateBytes {
						err = ErrInvalid
					}
				}
				if ctx.Err() != nil {
					if fatal == nil {
						fatal = ctx.Err()
					}
					cancel()
				} else if err != nil {
					if !job.call.Required && transportUnavailable(err) {
						failures[job.route] = append(failures[job.route], job)
					} else {
						if fatal == nil {
							fatal = ErrInvalid
						}
						cancel()
					}
				} else {
					results[job.route] = append(results[job.route], dispatchSuccess{job, PartitionResult{Target: job.call.Target, Candidates: candidates}})
				}
				mu.Unlock()
			}
		}()
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return dispatchResult{}, err
	}
	if fatal != nil {
		return dispatchResult{}, fatal
	}
	return dispatchResult{results, failures}, nil
}
