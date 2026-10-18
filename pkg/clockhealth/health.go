// Package clockhealth provides a small fail-closed wall/monotonic clock guard.
// It does not synchronize clocks or grant time tolerance to business leases.
package clockhealth

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrUnhealthy = errors.New("clock health unavailable")

type Observation struct {
	Samples             int
	Unstable            bool
	MaxAbsSkew, MaxStep time.Duration
}
type Tracker struct {
	firstWall, lastWall       time.Time
	firstElapsed, lastElapsed time.Duration
	Observation               Observation
	Tolerance                 time.Duration
}

func NewTracker(tolerance time.Duration) (*Tracker, error) {
	if tolerance <= 0 || tolerance > time.Second {
		return nil, ErrUnhealthy
	}
	now := time.Now()
	return &Tracker{firstWall: now.UTC(), lastWall: now.UTC(), Tolerance: tolerance, Observation: Observation{Samples: 1}}, nil
}
func (t *Tracker) Observe(now time.Time, elapsed time.Duration) error {
	if t == nil || now.IsZero() || elapsed < 0 || elapsed < t.lastElapsed {
		return ErrUnhealthy
	}
	wall := now.UTC()
	step := wall.Sub(t.lastWall) - (elapsed - t.lastElapsed)
	total := wall.Sub(t.firstWall) - (elapsed - t.firstElapsed)
	t.Observation.Samples++
	if abs(total) > t.Observation.MaxAbsSkew {
		t.Observation.MaxAbsSkew = abs(total)
	}
	if abs(step) > t.Observation.MaxStep {
		t.Observation.MaxStep = abs(step)
	}
	if abs(step) >= t.Tolerance || abs(total) >= t.Tolerance {
		t.Observation.Unstable = true
	}
	t.lastWall = wall
	t.lastElapsed = elapsed
	return nil
}
func (t *Tracker) Healthy() bool {
	return t != nil && !t.Observation.Unstable && t.Observation.Samples >= 2
}
func abs(v time.Duration) time.Duration {
	if v < 0 {
		return -v
	}
	return v
}

type Monitor struct {
	stop                chan struct{}
	done                chan struct{}
	once                sync.Once
	mu                  sync.RWMutex
	result              Observation
	interval, tolerance time.Duration
}

func Start(interval, tolerance time.Duration) (*Monitor, error) {
	if interval <= 0 || interval > time.Second {
		return nil, ErrUnhealthy
	}
	if _, e := NewTracker(tolerance); e != nil {
		return nil, e
	}
	m := &Monitor{stop: make(chan struct{}), done: make(chan struct{}), interval: interval, tolerance: tolerance}
	go m.run()
	return m, nil
}
func (m *Monitor) run() {
	start := time.Now()
	tracker, _ := NewTracker(m.tolerance)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			_ = tracker.Observe(now, now.Sub(start))
		case <-m.stop:
			now := time.Now()
			_ = tracker.Observe(now, now.Sub(start))
			m.mu.Lock()
			m.result = tracker.Observation
			m.mu.Unlock()
			close(m.done)
			return
		}
	}
}
func (m *Monitor) Stop() Observation {
	if m == nil {
		return Observation{}
	}
	m.once.Do(func() { close(m.stop) })
	<-m.done
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.result
}
func Check(ctx context.Context, health func(context.Context) error) error {
	if ctx == nil || health == nil || ctx.Err() != nil {
		return ErrUnhealthy
	}
	check, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if e := health(check); e != nil || check.Err() != nil {
		return ErrUnhealthy
	}
	return nil
}
