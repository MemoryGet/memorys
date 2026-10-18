package events

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type cursorStore struct {
	cursor string
	saves  int
	err    error
}

func (s *cursorStore) Load(context.Context, contracts.ID) (string, error) { return s.cursor, nil }
func (s *cursorStore) Save(_ context.Context, _ contracts.ID, cursor string) error {
	s.cursor = cursor
	s.saves++
	return s.err
}

func TestRunnerStopsIdempotentlyAfterBoundedDelivery(t *testing.T) {
	receipts := &receiptStore{seen: map[contracts.ID]bool{}}
	store := &cursorStore{}
	var calls atomic.Int32
	bus := busFunc(func(context.Context, contracts.EventConsumeRequest) (contracts.EventPage, error) {
		if calls.Add(1) == 1 {
			return contracts.EventPage{Events: []contracts.EventEnvelope{validEvent()}, NextCursor: "cursor-1"}, nil
		}
		return contracts.EventPage{NextCursor: "cursor-1"}, nil
	})
	r, err := NewRunner(RunnerConfig{Consumer: Consumer{Bus: bus, Receipts: receipts, Handle: func(context.Context, contracts.EventEnvelope) error { return nil }}, CursorStore: store, ConsumerID: "consumer", Limit: 1, Interval: time.Millisecond, CycleTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err = r.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = r.Stop(); err != nil {
		t.Fatal(err)
	}
	if r.Cursor() != "cursor-1" || store.cursor != "cursor-1" || store.saves != 1 || len(receipts.appended) != 1 {
		t.Fatalf("cursor=%q receipts=%d", r.Cursor(), len(receipts.appended))
	}
	snapshot := r.Snapshot()
	if !snapshot.Started || !snapshot.Stopped || snapshot.Cursor != "cursor-1" || snapshot.LastErr != nil {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestRunnerReportsDeliveryFailureAndKeepsCursor(t *testing.T) {
	receipts := &receiptStore{seen: map[contracts.ID]bool{}}
	store := &cursorStore{}
	var reported atomic.Int32
	bus := busFunc(func(context.Context, contracts.EventConsumeRequest) (contracts.EventPage, error) {
		return contracts.EventPage{Events: []contracts.EventEnvelope{validEvent()}, NextCursor: "cursor-1"}, nil
	})
	r, err := NewRunner(RunnerConfig{Consumer: Consumer{Bus: bus, Receipts: receipts, Handle: func(context.Context, contracts.EventEnvelope) error { return errors.New("handler") }}, CursorStore: store, ConsumerID: "consumer", Limit: 1, Interval: 10 * time.Millisecond, CycleTimeout: time.Second, OnError: func(error) { reported.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
	if err = r.Stop(); err == nil {
		t.Fatal("expected last delivery error")
	}
	if reported.Load() == 0 || r.Cursor() != "" || store.saves != 0 {
		t.Fatalf("reported=%d cursor=%q saves=%d", reported.Load(), r.Cursor(), store.saves)
	}
}

func TestRunnerContainsAlertHookPanic(t *testing.T) {
	receipts := &receiptStore{seen: map[contracts.ID]bool{}}
	store := &cursorStore{}
	bus := busFunc(func(context.Context, contracts.EventConsumeRequest) (contracts.EventPage, error) {
		return contracts.EventPage{Events: []contracts.EventEnvelope{validEvent()}}, nil
	})
	r, err := NewRunner(RunnerConfig{Consumer: Consumer{Bus: bus, Receipts: receipts, Handle: func(context.Context, contracts.EventEnvelope) error { return errors.New("handler") }}, CursorStore: store, ConsumerID: "consumer", Limit: 1, Interval: 10 * time.Millisecond, CycleTimeout: time.Second, OnError: func(error) { panic("alert backend") }})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if err = r.Stop(); err == nil {
		t.Fatal("expected delivery error, not alert panic")
	}
}

func TestRunnerReportsCursorPersistenceFailureAndKeepsRetrying(t *testing.T) {
	receipts := &receiptStore{seen: map[contracts.ID]bool{}}
	store := &cursorStore{err: errors.New("cursor store unavailable")}
	var reported atomic.Int32
	bus := busFunc(func(context.Context, contracts.EventConsumeRequest) (contracts.EventPage, error) {
		return contracts.EventPage{Events: []contracts.EventEnvelope{validEvent()}, NextCursor: "cursor-1"}, nil
	})
	r, err := NewRunner(RunnerConfig{
		Consumer:    Consumer{Bus: bus, Receipts: receipts, Handle: func(context.Context, contracts.EventEnvelope) error { return nil }},
		CursorStore: store, ConsumerID: "consumer", Limit: 1, Interval: time.Millisecond, CycleTimeout: time.Second,
		OnError: func(error) { reported.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for reported.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if reported.Load() == 0 || r.Cursor() != "" || len(receipts.appended) != 1 {
		t.Fatalf("persistence failure was not reported/replayed: reported=%d cursor=%q receipts=%d", reported.Load(), r.Cursor(), len(receipts.appended))
	}
	if err = r.Stop(); err == nil {
		t.Fatal("expected cursor persistence error")
	}
}

func TestRunnerStopsDuringCursorRetryBackoff(t *testing.T) {
	receipts := &receiptStore{seen: map[contracts.ID]bool{}}
	store := &cursorStore{err: errors.New("cursor store unavailable")}
	failed := make(chan struct{}, 1)
	bus := busFunc(func(context.Context, contracts.EventConsumeRequest) (contracts.EventPage, error) {
		return contracts.EventPage{Events: []contracts.EventEnvelope{validEvent()}, NextCursor: "cursor-1"}, nil
	})
	r, err := NewRunner(RunnerConfig{
		Consumer:    Consumer{Bus: bus, Receipts: receipts, Handle: func(context.Context, contracts.EventEnvelope) error { return nil }},
		CursorStore: store, ConsumerID: "consumer", Limit: 1, Interval: time.Second, CycleTimeout: time.Second,
		OnError: func(error) {
			select {
			case failed <- struct{}{}:
			default:
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("cursor failure did not occur")
	}
	started := time.Now()
	if err = r.Stop(); err == nil {
		t.Fatal("expected cursor persistence error")
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("stop waited for retry interval: %s", elapsed)
	}
}
