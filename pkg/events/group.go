package events

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrRunnerGroup = errors.New("event runner group unavailable")

// RunnerGroup owns the lifecycle of several independently durable consumers.
// Names are stable deployment identities; startup order is deterministic and
// a later failure rolls back runners already started in that call.
type RunnerGroup struct {
	mu      sync.Mutex
	runners map[contracts.ID]*Runner
	started bool
	stopped bool
}

func NewRunnerGroup(runners map[contracts.ID]*Runner) (*RunnerGroup, error) {
	if len(runners) == 0 || len(runners) > 32 {
		return nil, ErrRunnerGroup
	}
	owned := make(map[contracts.ID]*Runner, len(runners))
	for id, runner := range runners {
		if id == "" || runner == nil || owned[id] != nil {
			return nil, ErrRunnerGroup
		}
		owned[id] = runner
	}
	return &RunnerGroup{runners: owned}, nil
}

func (g *RunnerGroup) orderedIDs() []contracts.ID {
	ids := make([]contracts.ID, 0, len(g.runners))
	for id := range g.runners {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (g *RunnerGroup) Start(ctx context.Context) error {
	if g == nil || ctx == nil {
		return ErrRunnerGroup
	}
	g.mu.Lock()
	if g.started || g.stopped {
		g.mu.Unlock()
		return ErrRunnerGroup
	}
	ids := g.orderedIDs()
	g.started = true
	g.mu.Unlock()

	started := make([]*Runner, 0, len(ids))
	for _, id := range ids {
		runner := g.runners[id]
		if err := runner.Start(ctx); err != nil {
			for i := len(started) - 1; i >= 0; i-- {
				_ = started[i].Stop()
			}
			g.mu.Lock()
			g.started = false
			g.stopped = true
			g.mu.Unlock()
			return ErrRunnerGroup
		}
		started = append(started, runner)
	}
	return nil
}

func (g *RunnerGroup) Stop() error {
	if g == nil {
		return ErrRunnerGroup
	}
	g.mu.Lock()
	if !g.started {
		g.stopped = true
		g.mu.Unlock()
		return nil
	}
	ids := g.orderedIDs()
	g.stopped = true
	g.mu.Unlock()

	var stopErr error
	for i := len(ids) - 1; i >= 0; i-- {
		stopErr = errors.Join(stopErr, g.runners[ids[i]].Stop())
	}
	return stopErr
}

func (g *RunnerGroup) Ready() error {
	if g == nil {
		return ErrRunnerGroup
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.started || g.stopped {
		return ErrRunnerGroup
	}
	for _, runner := range g.runners {
		snapshot := runner.Snapshot()
		if !snapshot.Started || snapshot.Stopped || snapshot.LastErr != nil {
			return ErrRunnerGroup
		}
	}
	return nil
}

func (g *RunnerGroup) Snapshot() map[contracts.ID]Snapshot {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[contracts.ID]Snapshot, len(g.runners))
	for id, runner := range g.runners {
		out[id] = runner.Snapshot()
	}
	return out
}
