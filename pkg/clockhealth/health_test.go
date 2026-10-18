package clockhealth

import (
	"context"
	"testing"
	"time"
)

func TestTrackerAcceptsMonotonicSchedulingDelay(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr, e := NewTracker(100 * time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	tr.firstWall = base
	tr.lastWall = base
	tr.firstElapsed = 0
	tr.lastElapsed = 0
	if e = tr.Observe(base.Add(10*time.Second), 10*time.Second); e != nil || tr.Observation.Unstable {
		t.Fatal(e, tr.Observation)
	}
	if !tr.Healthy() {
		t.Fatal("tracker not healthy")
	}
}
func TestTrackerRejectsWallJump(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr, _ := NewTracker(100 * time.Millisecond)
	tr.firstWall = base
	tr.lastWall = base
	tr.firstElapsed = 0
	tr.lastElapsed = 0
	if e := tr.Observe(base.Add(5*time.Second), time.Second); e != nil || !tr.Observation.Unstable || tr.Observation.MaxStep != 4*time.Second {
		t.Fatal(e, tr.Observation)
	}
}
func TestMonitorStopsAndCheckFailsClosed(t *testing.T) {
	m, e := Start(time.Millisecond, 100*time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	obs := m.Stop()
	second := m.Stop()
	if obs.Samples < 2 || obs != second {
		t.Fatal(obs)
	}
	if Check(context.Background(), nil) == nil {
		t.Fatal("nil health accepted")
	}
	if Check(context.Background(), func(context.Context) error { return nil }) != nil {
		t.Fatal("healthy callback rejected")
	}
	deadlineSeen := false
	if Check(context.Background(), func(ctx context.Context) error { _, deadlineSeen = ctx.Deadline(); return nil }) != nil || !deadlineSeen {
		t.Fatal("health callback was not bounded")
	}
}
