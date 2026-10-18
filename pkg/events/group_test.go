package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type groupBus struct{}

func (groupBus) Publish(context.Context, contracts.EventEnvelope) error { return nil }
func (groupBus) Consume(_ context.Context, q contracts.EventConsumeRequest) (contracts.EventPage, error) {
	return contracts.EventPage{NextCursor: q.Cursor}, nil
}

type groupReceipts struct{}

func (groupReceipts) Contains(context.Context, contracts.ID, contracts.ID) (bool, error) {
	return false, nil
}
func (groupReceipts) Append(context.Context, contracts.ConsumerReceipt) error { return nil }

type groupStore struct{ loadErr error }

func (s groupStore) Load(context.Context, contracts.ID) (string, error) { return "", s.loadErr }
func (groupStore) Save(context.Context, contracts.ID, string) error     { return nil }

func newGroupRunner(t *testing.T, store CursorStore) *Runner {
	t.Helper()
	runner, err := NewRunner(RunnerConfig{
		Consumer: Consumer{
			Bus:      groupBus{},
			Receipts: groupReceipts{},
			Handle:   func(context.Context, contracts.EventEnvelope) error { return nil },
		},
		ConsumerID:   contracts.ID("consumer-" + t.Name()),
		CursorStore:  store,
		Limit:        1,
		Interval:     time.Millisecond,
		CycleTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestRunnerGroupStartsStopsAndReportsReady(t *testing.T) {
	group, err := NewRunnerGroup(map[contracts.ID]*Runner{
		"cleanup": newGroupRunner(t, groupStore{}),
		"publish": newGroupRunner(t, groupStore{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = group.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = group.Ready(); err != nil {
		t.Fatal("group not ready", err)
	}
	if snapshots := group.Snapshot(); len(snapshots) != 2 {
		t.Fatal("unexpected snapshot count", len(snapshots))
	}
	if err = group.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = group.Stop(); err != nil {
		t.Fatal("group stop was not idempotent", err)
	}
	if err = group.Ready(); !errors.Is(err, ErrRunnerGroup) {
		t.Fatal("stopped group reported ready", err)
	}
}

func TestRunnerGroupRollsBackEarlierRunnersOnStartupFailure(t *testing.T) {
	first := newGroupRunner(t, groupStore{})
	second := newGroupRunner(t, groupStore{loadErr: errors.New("cursor unavailable")})
	group, err := NewRunnerGroup(map[contracts.ID]*Runner{"a": first, "b": second})
	if err != nil {
		t.Fatal(err)
	}
	if err = group.Start(context.Background()); !errors.Is(err, ErrRunnerGroup) {
		t.Fatal("startup failure was not reported", err)
	}
	if snapshot := first.Snapshot(); !snapshot.Stopped {
		t.Fatal("earlier runner was not rolled back", snapshot)
	}
	if err = group.Start(context.Background()); !errors.Is(err, ErrRunnerGroup) {
		t.Fatal("failed group became restartable", err)
	}
}

func TestNewRunnerGroupRejectsInvalidMembers(t *testing.T) {
	if _, err := NewRunnerGroup(nil); !errors.Is(err, ErrRunnerGroup) {
		t.Fatal("empty runner group accepted")
	}
	if _, err := NewRunnerGroup(map[contracts.ID]*Runner{"nil": nil}); !errors.Is(err, ErrRunnerGroup) {
		t.Fatal("nil runner accepted")
	}
}
