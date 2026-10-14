package reflection_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/reflection"
	"testing"
	"time"
)

type reflector struct {
	assess func(context.Context, contracts.ReflectionRequest) (contracts.Assessment, error)
	refine func(context.Context, contracts.ReflectionRequest) (contracts.QueryPlan, error)
}

func (r reflector) Assess(c context.Context, q contracts.ReflectionRequest) (contracts.Assessment, error) {
	return r.assess(c, q)
}
func (r reflector) Refine(c context.Context, q contracts.ReflectionRequest) (contracts.QueryPlan, error) {
	return r.refine(c, q)
}
func (reflector) Propose(context.Context, contracts.ReflectionRequest) (contracts.DraftProposal, error) {
	panic("loop must never propose or publish")
}
func fixture(t *testing.T) (context.Context, contracts.ReflectionRequest, contracts.QueryProfile) {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	d, _ := c.Deadline()
	return c, contracts.ReflectionRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: d}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "user", KnowledgeBaseIDs: []contracts.ID{"kb"}, ExpiresAt: d}, Question: "问题", Budget: contracts.ExecutionBudget{Deadline: d, MaxOutputTokens: 100, MaxExtraRounds: 2}}, contracts.QueryProfile{Reflection: contracts.ReflectionPolicy{Enabled: true, MaxExtraRounds: 2, MaxPlanQueries: 2, TotalBudgetMS: 30000}, Routes: []contracts.RetrievalRoute{{ID: "route"}}}
}
func evidence(round int) []contracts.Evidence {
	return []contracts.Evidence{{Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: contracts.ID(fmt.Sprint("block", round))}, SourceExcerpt: "正文", Rank: 1}}
}
func gate(context.Context, contracts.ReflectionRequest) error { return nil }

func TestLoopRefinesThenSufficientWithoutProposal(t *testing.T) {
	c, q, p := fixture(t)
	assesses, refines, searches := 0, 0, 0
	r := reflector{assess: func(context.Context, contracts.ReflectionRequest) (contracts.Assessment, error) {
		assesses++
		return contracts.Assessment{Sufficient: assesses == 2}, nil
	}, refine: func(context.Context, contracts.ReflectionRequest) (contracts.QueryPlan, error) {
		refines++
		return contracts.QueryPlan{Queries: []contracts.PlannedQuery{{Text: "改写", RouteIDs: []contracts.ID{"route"}}}}, nil
	}}
	out, e := reflection.Run(c, q, p, r, func(_ context.Context, n int, _ contracts.QueryPlan) ([]contracts.Evidence, error) {
		searches++
		return evidence(n), nil
	}, gate)
	if e != nil || out.Status != "sufficient" || out.ExtraRounds != 1 || searches != 2 || refines != 1 {
		t.Fatal(out.Status, e, searches, refines)
	}
}

func TestLoopStopsAtTwoExtraRounds(t *testing.T) {
	c, q, p := fixture(t)
	refines := 0
	r := reflector{assess: func(context.Context, contracts.ReflectionRequest) (contracts.Assessment, error) {
		return contracts.Assessment{}, nil
	}, refine: func(context.Context, contracts.ReflectionRequest) (contracts.QueryPlan, error) {
		refines++
		return contracts.QueryPlan{Queries: []contracts.PlannedQuery{{Text: fmt.Sprint("variant", refines), RouteIDs: []contracts.ID{"route"}}}}, nil
	}}
	out, e := reflection.Run(c, q, p, r, func(_ context.Context, n int, _ contracts.QueryPlan) ([]contracts.Evidence, error) {
		return evidence(n), nil
	}, gate)
	if e != nil || out.ExtraRounds != 2 || refines != 2 || out.StopReason != "round_limit" {
		t.Fatal(out, e)
	}
}

func TestLoopRepeatedPlanAndNoNewEvidenceStop(t *testing.T) {
	for _, repeat := range []bool{true, false} {
		c, q, p := fixture(t)
		text := "changed"
		if repeat {
			text = q.Question
		}
		r := reflector{assess: func(context.Context, contracts.ReflectionRequest) (contracts.Assessment, error) {
			return contracts.Assessment{}, nil
		}, refine: func(context.Context, contracts.ReflectionRequest) (contracts.QueryPlan, error) {
			return contracts.QueryPlan{Queries: []contracts.PlannedQuery{{Text: text, RouteIDs: []contracts.ID{"route"}}}}, nil
		}}
		out, e := reflection.Run(c, q, p, r, func(context.Context, int, contracts.QueryPlan) ([]contracts.Evidence, error) { return evidence(0), nil }, gate)
		want := "no_new_evidence"
		if repeat {
			want = "repeated_plan"
		}
		if e != nil || out.StopReason != want {
			t.Fatal(out.StopReason, e)
		}
	}
}

func TestLoopAuthorityFailureDoesNotReachModel(t *testing.T) {
	c, q, p := fixture(t)
	models := 0
	r := reflector{assess: func(context.Context, contracts.ReflectionRequest) (contracts.Assessment, error) {
		models++
		return contracts.Assessment{}, nil
	}}
	out, e := reflection.Run(c, q, p, r, func(context.Context, int, contracts.QueryPlan) ([]contracts.Evidence, error) { return evidence(0), nil }, func(_ context.Context, r contracts.ReflectionRequest) error {
		if len(r.Evidence) > 0 {
			return errors.New("private revoke detail")
		}
		return nil
	})
	if e != reflection.ErrAuthority || models != 0 || len(out.Evidence) != 0 {
		t.Fatal(e, models)
	}
}

func TestLoopDeadlineCancellationAndMalformedPlan(t *testing.T) {
	c, q, p := fixture(t)
	short, cancel := context.WithTimeout(c, 30*time.Millisecond)
	defer cancel()
	r := reflector{assess: func(c context.Context, _ contracts.ReflectionRequest) (contracts.Assessment, error) {
		<-c.Done()
		return contracts.Assessment{}, c.Err()
	}}
	out, e := reflection.Run(short, q, p, r, func(context.Context, int, contracts.QueryPlan) ([]contracts.Evidence, error) { return evidence(0), nil }, gate)
	if !errors.Is(e, context.DeadlineExceeded) || len(out.Evidence) != 0 {
		t.Fatal(e)
	}
	c, q, p = fixture(t)
	r = reflector{assess: func(context.Context, contracts.ReflectionRequest) (contracts.Assessment, error) {
		return contracts.Assessment{}, nil
	}, refine: func(context.Context, contracts.ReflectionRequest) (contracts.QueryPlan, error) {
		return contracts.QueryPlan{Queries: []contracts.PlannedQuery{{Text: "rewrite", RouteIDs: []contracts.ID{"unknown"}}}}, nil
	}}
	if _, e := reflection.Run(c, q, p, r, func(context.Context, int, contracts.QueryPlan) ([]contracts.Evidence, error) { return evidence(0), nil }, gate); e != reflection.ErrInvalid {
		t.Fatal("invalid plan accepted", e)
	}
}

func TestLoopModelInputCopiesAndDisabledReflection(t *testing.T) {
	c, q, p := fixture(t)
	r := reflector{assess: func(_ context.Context, v contracts.ReflectionRequest) (contracts.Assessment, error) {
		v.Scope.KnowledgeBaseIDs[0] = "other"
		v.Evidence[0].SourceExcerpt = "changed"
		return contracts.Assessment{Sufficient: true}, nil
	}}
	out, e := reflection.Run(c, q, p, r, func(context.Context, int, contracts.QueryPlan) ([]contracts.Evidence, error) { return evidence(0), nil }, gate)
	if e != nil || out.Evidence[0].SourceExcerpt != "正文" || q.Scope.KnowledgeBaseIDs[0] != "kb" {
		t.Fatal("model altered platform input", e)
	}
	p.Reflection.Enabled = false
	out, e = reflection.Run(c, q, p, nil, func(context.Context, int, contracts.QueryPlan) ([]contracts.Evidence, error) { return evidence(0), nil }, gate)
	if e != nil || out.Status != "retrieved" {
		t.Fatal(e)
	}
}

func TestLoopRechecksAuthorityAfterRefineBeforeReturningEvidence(t *testing.T) {
	c, q, p := fixture(t)
	revoked := false
	r := reflector{assess: func(context.Context, contracts.ReflectionRequest) (contracts.Assessment, error) {
		return contracts.Assessment{}, nil
	}, refine: func(context.Context, contracts.ReflectionRequest) (contracts.QueryPlan, error) {
		revoked = true
		return contracts.QueryPlan{Queries: []contracts.PlannedQuery{{Text: q.Question, RouteIDs: []contracts.ID{"route"}}}}, nil
	}}
	out, err := reflection.Run(c, q, p, r, func(context.Context, int, contracts.QueryPlan) ([]contracts.Evidence, error) { return evidence(0), nil }, func(context.Context, contracts.ReflectionRequest) error {
		if revoked {
			return errors.New("revoked during model call")
		}
		return nil
	})
	if err != reflection.ErrAuthority || len(out.Evidence) != 0 {
		t.Fatal("post-refine revoke leaked evidence", err)
	}
}

func TestLoopCannotExpandCallerRoundBudget(t *testing.T) {
	c, q, p := fixture(t)
	q.Budget.MaxExtraRounds = 0
	r := reflector{assess: func(context.Context, contracts.ReflectionRequest) (contracts.Assessment, error) {
		return contracts.Assessment{}, nil
	}, refine: func(context.Context, contracts.ReflectionRequest) (contracts.QueryPlan, error) {
		t.Fatal("caller forbade additional rounds")
		return contracts.QueryPlan{}, nil
	}}
	out, err := reflection.Run(c, q, p, r, func(context.Context, int, contracts.QueryPlan) ([]contracts.Evidence, error) { return evidence(0), nil }, gate)
	if err != nil || out.ExtraRounds != 0 || out.StopReason != "round_limit" {
		t.Fatal("caller budget expanded", err)
	}
}

func TestLoopPolicyTimeBudgetCapsLongerParent(t *testing.T) {
	c, q, p := fixture(t)
	p.Reflection.TotalBudgetMS = 100
	parent, _ := c.Deadline()
	_, err := reflection.Run(c, q, p, reflector{}, func(child context.Context, _ int, _ contracts.QueryPlan) ([]contracts.Evidence, error) {
		d, _ := child.Deadline()
		if !d.Before(parent) {
			t.Fatal("policy horizon did not narrow parent")
		}
		<-child.Done()
		return nil, child.Err()
	}, gate)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("policy timeout not propagated", err)
	}
}
