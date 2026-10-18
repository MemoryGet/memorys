package main

import (
	"reflect"
	"testing"
	"time"
)

func TestFixtureClockDistinguishesSchedulingFromWallSteps(t *testing.T) {
	base := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	normal := newFixtureClockTracker(fixtureClockPair{wall: base})
	normal.observe(fixtureClockPair{wall: base.Add(10 * time.Second), elapsed: 10 * time.Second})
	if normal.observation.Unstable {
		t.Fatal("scheduler delay mistaken for wall step")
	}
	for _, step := range []time.Duration{-4 * time.Second, 4 * time.Second} {
		tracker := newFixtureClockTracker(fixtureClockPair{wall: base})
		tracker.observe(fixtureClockPair{wall: base.Add(time.Second + step), elapsed: time.Second})
		if !tracker.observation.Unstable || tracker.observation.Events[0].Step != step || tracker.observation.MaxAbsSkew != clockAbs(step) {
			t.Fatal("wall step not observed", tracker.observation)
		}
	}
}
func TestFixtureClockCumulativeSkewAndEvidenceBound(t *testing.T) {
	base := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	tracker := newFixtureClockTracker(fixtureClockPair{wall: base})
	for i := 1; i <= 3; i++ {
		tracker.observe(fixtureClockPair{wall: base.Add(time.Duration(i) * 1050 * time.Millisecond), elapsed: time.Duration(i) * time.Second})
	}
	if !tracker.observation.Unstable || tracker.observation.MaxAbsSkew != 150*time.Millisecond {
		t.Fatal("cumulative skew missed")
	}
	for i := 4; i < 20; i++ {
		tracker.observe(fixtureClockPair{wall: base.Add(time.Duration(i) * 2 * time.Second), elapsed: time.Duration(i) * time.Second})
	}
	if len(tracker.observation.Events) != fixtureClockEventLimit || tracker.observation.EventCount <= fixtureClockEventLimit {
		t.Fatal("clock evidence not bounded")
	}
}
func TestFixtureClockMonitorStopsAndJoins(t *testing.T) {
	monitor := startFixtureClockMonitor()
	first := monitor.Stop()
	second := monitor.Stop()
	if first.Samples < 2 || !reflect.DeepEqual(first, second) {
		t.Fatal("clock observer not joined")
	}
}
