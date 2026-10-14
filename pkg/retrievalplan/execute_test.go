package retrievalplan_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/retrievalplan"
)

func executionPlan(t *testing.T) retrievalplan.Plan {
	t.Helper()
	p, e := retrievalplan.Build(profile(), inventory(), limits)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func executionContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestExecutorPoolBoundsAndWaitsForWorkers(t *testing.T) {
	p := executionPlan(t)
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var active, peak, completed atomic.Int32
	done := make(chan error, 1)
	ctx := executionContext(t)
	go func() {
		_, err := retrievalplan.Execute(ctx, p, func(ctx context.Context, _ retrievalplan.Call) ([]contracts.Candidate, error) {
			n := active.Add(1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			active.Add(-1)
			completed.Add(1)
			return nil, nil
		})
		done <- err
	}()
	for i := 0; i < p.MaxParallel; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	if active.Load() != 2 || peak.Load() > 2 {
		t.Fatal("pool exceeded configured bound")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 2 || completed.Load() != 5 || active.Load() != 0 {
		t.Fatal("workers not fully joined")
	}
}

func TestOptionalTransportFailureDiscardsEntireRoute(t *testing.T) {
	p := executionPlan(t)
	p.MaxParallel = 1
	var mixed atomic.Int32
	out, err := retrievalplan.Execute(executionContext(t), p, func(_ context.Context, c retrievalplan.Call) ([]contracts.Candidate, error) {
		if c.RouteID == "mixed" {
			if mixed.Add(1) == 1 {
				return nil, nil
			}
			return nil, retrievalplan.ErrUnavailable
		}
		return nil, nil
	})
	if err != nil || len(out.Degraded) != 1 || out.Degraded[0].RouteID != "mixed" || len(out.Routes) != 1 || out.Routes[0].List.RouteID != "vector" || mixed.Load() != 2 {
		t.Fatal("wrong route degradation", out, err)
	}
}

func TestExecutorDeadlineStopsQueueWithoutPartialOutput(t *testing.T) {
	p := executionPlan(t)
	p.MaxParallel = 1
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var calls atomic.Int32
	out, err := retrievalplan.Execute(ctx, p, func(c context.Context, _ retrievalplan.Call) ([]contracts.Candidate, error) {
		calls.Add(1)
		<-c.Done()
		return nil, retrievalplan.ErrUnavailable
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 || len(out.Routes) != 0 || len(out.Degraded) != 0 {
		t.Fatal("deadline was downgraded or queued calls started", out, err)
	}
}

func TestRequiredFailureAndContractFailureAreFatal(t *testing.T) {
	for _, search := range []retrievalplan.SearchFunc{
		func(_ context.Context, c retrievalplan.Call) ([]contracts.Candidate, error) {
			if c.Required {
				return nil, retrievalplan.ErrUnavailable
			}
			return nil, nil
		},
		func(_ context.Context, _ retrievalplan.Call) ([]contracts.Candidate, error) {
			return nil, errors.New("private authorization detail")
		},
		func(_ context.Context, _ retrievalplan.Call) ([]contracts.Candidate, error) {
			return []contracts.Candidate{{KnowledgeBaseID: "unauthorized"}}, nil
		},
		func(context.Context, retrievalplan.Call) ([]contracts.Candidate, error) {
			panic("private backend detail")
		},
	} {
		out, err := retrievalplan.Execute(executionContext(t), executionPlan(t), search)
		if err != retrievalplan.ErrInvalid || len(out.Routes) != 0 || len(out.Degraded) != 0 {
			t.Fatal("fatal error downgraded or exposed", out, err)
		}
	}
}

func TestCancellationStopsQueuedCallsAndJoinsActive(t *testing.T) {
	p := executionPlan(t)
	p.MaxParallel = 1
	ctx, cancel := context.WithCancel(executionContext(t))
	defer cancel()
	entered := make(chan struct{})
	done := make(chan error, 1)
	var calls, stopped atomic.Int32
	go func() {
		_, err := retrievalplan.Execute(ctx, p, func(c context.Context, _ retrievalplan.Call) ([]contracts.Candidate, error) {
			calls.Add(1)
			close(entered)
			<-c.Done()
			stopped.Add(1)
			return nil, c.Err()
		})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("call not entered")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls.Load() != 1 || stopped.Load() != 1 {
		t.Fatal("queued call executed or active worker escaped")
	}
}

func TestExecutorRejectsMalformedPlanBeforeCalling(t *testing.T) {
	for _, change := range []func(*retrievalplan.Plan){func(p *retrievalplan.Plan) { p.MaxParallel = 33 }, func(p *retrievalplan.Plan) { p.Routes[0].Calls[0].Limit++ }, func(p *retrievalplan.Plan) { p.Routes[1].ID = p.Routes[0].ID }, func(p *retrievalplan.Plan) { p.CandidateLimit = 1 }} {
		p := executionPlan(t)
		change(&p)
		called := false
		if _, err := retrievalplan.Execute(executionContext(t), p, func(context.Context, retrievalplan.Call) ([]contracts.Candidate, error) {
			called = true
			return nil, nil
		}); err == nil || called {
			t.Fatal("malformed plan invoked backend")
		}
	}
	if _, err := retrievalplan.Execute(context.Background(), executionPlan(t), func(context.Context, retrievalplan.Call) ([]contracts.Candidate, error) {
		t.Fatal("unbounded context invoked")
		return nil, nil
	}); err == nil {
		t.Fatal("missing deadline accepted")
	}
}

func TestMixedTransportAndContractErrorCannotDegrade(t *testing.T) {
	out, err := retrievalplan.Execute(executionContext(t), executionPlan(t), func(_ context.Context, call retrievalplan.Call) ([]contracts.Candidate, error) {
		if !call.Required {
			return nil, errors.Join(retrievalplan.ErrUnavailable, retrievalplan.ErrInvalid)
		}
		return nil, nil
	})
	if err != retrievalplan.ErrInvalid || len(out.Degraded) != 0 {
		t.Fatal("mixed failure downgraded", out, err)
	}
}

func TestExecutionSharesByteBudgetAcrossRoutes(t *testing.T) {
	p := executionPlan(t)
	p.MaxParallel = 1
	// Each route is individually below 16MiB, but together exceeds it.
	out, err := retrievalplan.Execute(executionContext(t), p, func(_ context.Context, call retrievalplan.Call) ([]contracts.Candidate, error) {
		return []contracts.Candidate{{RouteID: call.RouteID, RetrieverID: call.RetrieverID, KnowledgeBaseID: call.Target.KnowledgeBaseID, ChunkID: "chunk", Rank: 1, ScoreKind: "vendor", GenerationIDs: []contracts.ID{"gen"}, Text: strings.Repeat("x", 4<<20), Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block"}}}, nil
	})
	if err != retrievalplan.ErrInvalid || len(out.Routes) != 0 {
		t.Fatal("per-route limits allowed aggregate overflow", len(out.Routes), err)
	}
}

func TestSingleWrappedTransportFailureMayDegrade(t *testing.T) {
	out, err := retrievalplan.Execute(executionContext(t), executionPlan(t), func(_ context.Context, call retrievalplan.Call) ([]contracts.Candidate, error) {
		if !call.Required {
			return nil, fmt.Errorf("adapter transport: %w", retrievalplan.ErrUnavailable)
		}
		return nil, nil
	})
	if err != nil || len(out.Degraded) != 1 || len(out.Routes) != 1 {
		t.Fatal("single transport wrapper rejected", err)
	}
}

func TestExecutorPreservesDeploymentCallCap(t *testing.T) {
	for _, max := range []int{0, 4, 257} {
		p := executionPlan(t)
		p.MaxCalls = max
		var called atomic.Int32
		out, err := retrievalplan.Execute(executionContext(t), p, func(context.Context, retrievalplan.Call) ([]contracts.Candidate, error) {
			called.Add(1)
			return nil, nil
		})
		if err != retrievalplan.ErrInvalid || called.Load() != 0 || len(out.Routes) != 0 {
			t.Fatal("deployment cap ignored", max, err)
		}
	}
}

func TestBuiltPlanCarriesHostLimitsAndRunsAtExactCap(t *testing.T) {
	l := retrievalplan.Limits{MaxCalls: 5, MaxParallel: 2}
	p, err := retrievalplan.Build(profile(), inventory(), l)
	if err != nil || p.MaxCalls != l.MaxCalls || p.MaxParallel != l.MaxParallel {
		t.Fatal("host limits lost", p, err)
	}
	l.MaxCalls = 1
	var called atomic.Int32
	if _, err := retrievalplan.Execute(executionContext(t), p, func(context.Context, retrievalplan.Call) ([]contracts.Candidate, error) {
		called.Add(1)
		return nil, nil
	}); err != nil || called.Load() != 5 {
		t.Fatal("exact cap rejected or input changed captured limit", err, called.Load())
	}
}

func TestExecutorRejectsParallelismAboveCallCap(t *testing.T) {
	p := executionPlan(t)
	p.MaxCalls = 1
	p.MaxParallel = 2
	if _, err := retrievalplan.Execute(executionContext(t), p, func(context.Context, retrievalplan.Call) ([]contracts.Candidate, error) {
		t.Fatal("invalid concurrency invoked backend")
		return nil, nil
	}); err != retrievalplan.ErrInvalid {
		t.Fatal("inconsistent limits accepted", err)
	}
}
