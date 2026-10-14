package retrievalplan_test

import (
	"context"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/retrievalplan"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func refinedExecutionPlan(t *testing.T) retrievalplan.RefinementPlan {
	t.Helper()
	q, i, p := refinementFixture()
	r, e := retrievalplan.BuildRefinement(q, i, retrievalplan.Limits{MaxCalls: 10, MaxParallel: 2}, p)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestRefinementExecutorSharedPoolAndQueryMetadata(t *testing.T) {
	p := refinedExecutionPlan(t)
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	done := make(chan error, 1)
	var active, peak, count atomic.Int32
	ctx := executionContext(t)
	go func() {
		_, e := retrievalplan.ExecuteRefinement(ctx, p, func(c context.Context, j retrievalplan.RefinementCall) ([]contracts.Candidate, error) {
			if j.QueryText == "" || j.QueryIndex < 0 || j.QueryIndex > 1 {
				return nil, errors.New("missing query metadata")
			}
			n := active.Add(1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-c.Done():
			}
			active.Add(-1)
			count.Add(1)
			return nil, nil
		})
		done <- e
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	if peak.Load() != 2 {
		t.Fatal("variant pools did not share bound")
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if count.Load() != 10 || peak.Load() > 2 || active.Load() != 0 {
		t.Fatal("work not bounded/joined")
	}
}

func TestRefinementExecutorSharesBytesAcrossRoutesAndQueries(t *testing.T) {
	p := refinedExecutionPlan(t)
	p.MaxParallel = 1
	out, err := retrievalplan.ExecuteRefinement(executionContext(t), p, func(_ context.Context, j retrievalplan.RefinementCall) ([]contracts.Candidate, error) {
		return []contracts.Candidate{{RouteID: j.Call.RouteID, RetrieverID: j.Call.RetrieverID, KnowledgeBaseID: j.Call.Target.KnowledgeBaseID, ChunkID: "chunk", Rank: 1, ScoreKind: "vendor", GenerationIDs: []contracts.ID{"gen"}, Text: strings.Repeat("x", 2<<20), Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block"}}}, nil
	})
	if err != retrievalplan.ErrInvalid || len(out.Routes) != 0 || len(out.Degraded) != 0 {
		t.Fatal("whole-round byte budget bypassed", err)
	}
}

func TestRefinementExecutorCapturesPlanQueryCapAndCrossRouteText(t *testing.T) {
	for _, mutate := range []func(*retrievalplan.RefinementPlan){
		func(p *retrievalplan.RefinementPlan) { p.MaxPlanQueries = 1 },
		func(p *retrievalplan.RefinementPlan) {
			for i := range p.Routes[1].Calls {
				if p.Routes[1].Calls[i].QueryIndex == 0 {
					p.Routes[1].Calls[i].QueryText = "different query for same index"
				}
			}
		},
	} {
		p := refinedExecutionPlan(t)
		mutate(&p)
		var calls atomic.Int32
		_, err := retrievalplan.ExecuteRefinement(executionContext(t), p, func(context.Context, retrievalplan.RefinementCall) ([]contracts.Candidate, error) {
			calls.Add(1)
			return nil, nil
		})
		if err != retrievalplan.ErrInvalid || calls.Load() != 0 {
			t.Fatal("query cap/text mutation reached backend", err)
		}
	}
}

func TestRefinementExecutorDiscardsWholeOptionalRouteAcrossVariants(t *testing.T) {
	p := refinedExecutionPlan(t)
	p.MaxParallel = 1
	out, e := retrievalplan.ExecuteRefinement(executionContext(t), p, func(_ context.Context, j retrievalplan.RefinementCall) ([]contracts.Candidate, error) {
		if j.Call.RouteID == "mixed" && j.QueryIndex == 1 {
			return nil, retrievalplan.ErrUnavailable
		}
		return nil, nil
	})
	if e != nil || len(out.Routes) != 1 || out.Routes[0].List.RouteID != "vector" || len(out.Degraded) != 1 || out.Degraded[0].FailedCalls[0].QueryIndex != 1 {
		t.Fatal("partial optional route survived", e)
	}
}

func TestRefinementExecutorRequiredFailureAndMalformedPlanStop(t *testing.T) {
	p := refinedExecutionPlan(t)
	out, e := retrievalplan.ExecuteRefinement(executionContext(t), p, func(_ context.Context, j retrievalplan.RefinementCall) ([]contracts.Candidate, error) {
		if j.Call.Required {
			return nil, retrievalplan.ErrUnavailable
		}
		return nil, nil
	})
	if e == nil || len(out.Routes) != 0 {
		t.Fatal("required route failed open")
	}
	p.MaxCalls = 9
	var called atomic.Int32
	if _, e := retrievalplan.ExecuteRefinement(executionContext(t), p, func(context.Context, retrievalplan.RefinementCall) ([]contracts.Candidate, error) {
		called.Add(1)
		return nil, nil
	}); e != retrievalplan.ErrInvalid || called.Load() != 0 {
		t.Fatal("invalid fanout executed", e)
	}
}

func TestRefinementExecutorCancellationStopsQueuedVariants(t *testing.T) {
	p := refinedExecutionPlan(t)
	p.MaxParallel = 1
	c, cancel := context.WithCancel(executionContext(t))
	defer cancel()
	entered := make(chan struct{})
	done := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		_, e := retrievalplan.ExecuteRefinement(c, p, func(c context.Context, _ retrievalplan.RefinementCall) ([]contracts.Candidate, error) {
			calls.Add(1)
			close(entered)
			<-c.Done()
			return nil, c.Err()
		})
		done <- e
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("call not started")
	}
	cancel()
	if e := <-done; !errors.Is(e, context.Canceled) || calls.Load() != 1 {
		t.Fatal("queued query survived cancel", e, calls.Load())
	}
}
