// Package reflection provides evidence-side orchestration, not an authority or model.
package reflection

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid reflection input or result")
var ErrStage = errors.New("reflection stage failed")
var ErrAuthority = errors.New("reflection authority check failed")

type RoundSearch func(context.Context, int, contracts.QueryPlan) ([]contracts.Evidence, error)
type EvidenceGate func(context.Context, contracts.ReflectionRequest) error
type Result struct {
	Evidence           []contracts.Evidence
	Assessment         contracts.Assessment
	ExtraRounds        int
	Status, StopReason string
}

func copyValue[T any](v T) (T, error) {
	var out T
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > 4<<20 {
		return out, ErrInvalid
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, ErrInvalid
	}
	return out, nil
}
func planKey(plan contracts.QueryPlan) string {
	for i := range plan.Queries {
		plan.Queries[i].RouteIDs = append([]contracts.ID(nil), plan.Queries[i].RouteIDs...)
		sort.Slice(plan.Queries[i].RouteIDs, func(a, b int) bool { return plan.Queries[i].RouteIDs[a] < plan.Queries[i].RouteIDs[b] })
	}
	raw, _ := json.Marshal(plan)
	return string(raw)
}
func validPlan(p contracts.QueryPlan, known map[contracts.ID]bool, max int) bool {
	if len(p.Queries) < 1 || len(p.Queries) > max {
		return false
	}
	seen := map[string]bool{}
	for _, q := range p.Queries {
		if !utf8.ValidString(q.Text) || strings.TrimSpace(q.Text) == "" || utf8.RuneCountInString(q.Text) > 16000 || len(q.RouteIDs) < 1 || len(q.RouteIDs) > len(known) {
			return false
		}
		routes := map[contracts.ID]bool{}
		for _, id := range q.RouteIDs {
			if !known[id] || routes[id] {
				return false
			}
			routes[id] = true
		}
		key := planKey(contracts.QueryPlan{Queries: []contracts.PlannedQuery{q}})
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// Run executes one request-local loop. Search prepares fresh authority/quotas;
// Gate checks current state before data leaves for a model or caller. Callbacks
// must obey cancellation. No proposal, publication or generation is performed.
func Run(ctx context.Context, base contracts.ReflectionRequest, profile contracts.QueryProfile, model contracts.Reflector, search RoundSearch, gate EvidenceGate) (Result, error) {
	if ctx == nil || search == nil || gate == nil || base.Answer != nil || len(base.Evidence) != 0 || base.Request.RequestID == "" || base.Request.Deadline.IsZero() || base.Budget.Deadline.IsZero() || base.Scope.SubjectID == "" || base.Scope.ScopeID == "" || base.Scope.ExpiresAt.IsZero() || !utf8.ValidString(base.Question) || strings.TrimSpace(base.Question) == "" || utf8.RuneCountInString(base.Question) > 16000 {
		return Result{}, ErrInvalid
	}
	parent, ok := ctx.Deadline()
	if !ok {
		return Result{}, ErrInvalid
	}
	p := profile.Reflection
	if base.Budget.MaxExtraRounds < 0 || base.Budget.MaxExtraRounds > 2 {
		return Result{}, ErrInvalid
	}
	if p.MaxExtraRounds < 0 || p.MaxExtraRounds > 2 || p.MaxPlanQueries < 1 || p.MaxPlanQueries > 4 || p.TotalBudgetMS < 1 || p.TotalBudgetMS > 30000 || len(profile.Routes) < 1 || len(profile.Routes) > 32 || (p.Enabled && (model == nil || base.Budget.MaxOutputTokens < 1)) {
		return Result{}, ErrInvalid
	}
	deadline := time.Now().Add(time.Duration(p.TotalBudgetMS) * time.Millisecond)
	for _, d := range []time.Time{parent, base.Request.Deadline, base.Budget.Deadline, base.Scope.ExpiresAt} {
		if d.Before(deadline) {
			deadline = d
		}
	}
	child, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	base, err := copyValue(base)
	if err != nil {
		return Result{}, err
	}
	base.Request.Deadline = deadline
	base.Budget.Deadline = deadline
	maxRounds := p.MaxExtraRounds
	if base.Budget.MaxExtraRounds < maxRounds {
		maxRounds = base.Budget.MaxExtraRounds
	}
	base.Budget.MaxExtraRounds = maxRounds
	known := map[contracts.ID]bool{}
	var ids []contracts.ID
	for _, r := range profile.Routes {
		if r.ID == "" || known[r.ID] {
			return Result{}, ErrInvalid
		}
		known[r.ID] = true
		ids = append(ids, r.ID)
	}
	plan := contracts.QueryPlan{Queries: []contracts.PlannedQuery{{Text: base.Question, RouteIDs: ids}}}
	history := map[string]bool{}
	evidenceHistory := map[string]bool{}
	check := func(r contracts.ReflectionRequest) error {
		if !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
		if child.Err() != nil {
			return child.Err()
		}
		r, e := copyValue(r)
		if e != nil {
			return e
		}
		if gate(child, r) != nil {
			if child.Err() != nil {
				return child.Err()
			}
			return ErrAuthority
		}
		return child.Err()
	}
	for round := 0; ; round++ {
		if err := check(base); err != nil {
			return Result{}, err
		}
		history[planKey(plan)] = true
		requestPlan, e := copyValue(plan)
		if e != nil {
			return Result{}, e
		}
		evidence, e := search(child, round, requestPlan)
		if child.Err() != nil {
			return Result{}, child.Err()
		}
		if e != nil {
			return Result{}, ErrStage
		}
		if len(evidence) > 1000 {
			return Result{}, ErrInvalid
		}
		evidence, e = copyValue(evidence)
		if e != nil {
			return Result{}, e
		}
		novel := 0
		for _, item := range evidence {
			if item.Citation.DocumentID == "" || item.Citation.RevisionID == "" || item.Citation.BlockID == "" || !utf8.ValidString(item.SourceExcerpt) || strings.TrimSpace(item.SourceExcerpt) == "" || item.Rank < 1 {
				return Result{}, ErrInvalid
			}
			key, _ := json.Marshal(struct {
				Citation contracts.Citation
				Text     string
			}{item.Citation, item.SourceExcerpt})
			if !evidenceHistory[string(key)] {
				novel++
				evidenceHistory[string(key)] = true
			}
		}
		request := base
		request.Evidence = evidence
		if err := check(request); err != nil {
			return Result{}, err
		}
		out := Result{Evidence: evidence, ExtraRounds: round, Status: "insufficient"}
		if !p.Enabled {
			out.Status = "retrieved"
			out.StopReason = "reflection_disabled"
			return out, nil
		}
		if round > 0 && novel == 0 {
			out.StopReason = "no_new_evidence"
			return out, nil
		}
		if len(evidence) > 0 {
			modelRequest, e := copyValue(request)
			if e != nil {
				return Result{}, e
			}
			assessment, e := model.Assess(child, modelRequest)
			if child.Err() != nil {
				return Result{}, child.Err()
			}
			if e != nil {
				return Result{}, ErrStage
			}
			assessment, e = copyValue(assessment)
			if e != nil {
				return Result{}, e
			}
			out.Assessment = assessment
			if err := check(request); err != nil {
				return Result{}, err
			}
			if assessment.Sufficient {
				out.Status = "sufficient"
				out.StopReason = "assessment_sufficient"
				return out, nil
			}
		}
		if round == maxRounds {
			out.StopReason = "round_limit"
			return out, nil
		}
		if err := check(request); err != nil {
			return Result{}, err
		}
		modelRequest, e := copyValue(request)
		if e != nil {
			return Result{}, e
		}
		next, e := model.Refine(child, modelRequest)
		if child.Err() != nil {
			return Result{}, child.Err()
		}
		if e != nil {
			return Result{}, ErrStage
		}
		if err := check(request); err != nil {
			return Result{}, err
		}
		next, e = copyValue(next)
		if e != nil || !validPlan(next, known, p.MaxPlanQueries) {
			return Result{}, ErrInvalid
		}
		if history[planKey(next)] {
			out.StopReason = "repeated_plan"
			return out, nil
		}
		plan = next
	}
}
