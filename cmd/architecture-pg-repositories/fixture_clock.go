package main

import (
	"sync"
	"time"
)

const fixtureClockTolerance = 100 * time.Millisecond
const fixtureClockEventLimit = 4

type fixtureClockPair struct {
	wall    time.Time
	elapsed time.Duration
}
type fixtureClockEvent struct {
	Elapsed   time.Duration `json:"elapsed_ns"`
	Step      time.Duration `json:"step_ns"`
	TotalSkew time.Duration `json:"total_skew_ns"`
}
type fixtureClockObservation struct {
	Unstable   bool                `json:"unstable"`
	Samples    int                 `json:"samples"`
	Tolerance  time.Duration       `json:"tolerance_ns"`
	MaxAbsSkew time.Duration       `json:"max_abs_skew_ns"`
	EventCount int                 `json:"event_count"`
	Events     []fixtureClockEvent `json:"events"`
}
type fixtureClockTracker struct {
	first, last fixtureClockPair
	observation fixtureClockObservation
}

func newFixtureClockTracker(first fixtureClockPair) *fixtureClockTracker {
	return &fixtureClockTracker{first: first, last: first, observation: fixtureClockObservation{Samples: 1, Tolerance: fixtureClockTolerance, Events: []fixtureClockEvent{}}}
}
func clockAbs(v time.Duration) time.Duration {
	if v < 0 {
		return -v
	}
	return v
}
func (t *fixtureClockTracker) observe(current fixtureClockPair) {
	step := current.wall.Sub(t.last.wall) - (current.elapsed - t.last.elapsed)
	total := current.wall.Sub(t.first.wall) - (current.elapsed - t.first.elapsed)
	t.observation.Samples++
	if clockAbs(total) > t.observation.MaxAbsSkew {
		t.observation.MaxAbsSkew = clockAbs(total)
	}
	if clockAbs(step) >= fixtureClockTolerance || !t.observation.Unstable && clockAbs(total) >= fixtureClockTolerance {
		t.observation.Unstable = true
		t.observation.EventCount++
		if len(t.observation.Events) < fixtureClockEventLimit {
			t.observation.Events = append(t.observation.Events, fixtureClockEvent{Elapsed: current.elapsed, Step: step, TotalSkew: total})
		}
	}
	t.last = current
}

type fixtureClockMonitor struct {
	stop, done chan struct{}
	once       sync.Once
	result     fixtureClockObservation
}

func startFixtureClockMonitor() *fixtureClockMonitor {
	m := &fixtureClockMonitor{stop: make(chan struct{}), done: make(chan struct{})}
	start := time.Now()
	go func() {
		defer close(m.done)
		tracker := newFixtureClockTracker(fixtureClockPair{wall: start.UTC()})
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		sample := func() { now := time.Now(); tracker.observe(fixtureClockPair{wall: now.UTC(), elapsed: now.Sub(start)}) }
		for {
			select {
			case <-ticker.C:
				sample()
			case <-m.stop:
				sample()
				m.result = tracker.observation
				return
			}
		}
	}()
	return m
}
func (m *fixtureClockMonitor) Stop() fixtureClockObservation {
	m.once.Do(func() { close(m.stop) })
	<-m.done
	return m.result
}
