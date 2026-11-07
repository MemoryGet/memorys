package ingestion

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type parseQueueFake struct {
	lease     ParseTaskLease
	completed contracts.ID
	failed    contracts.PublicError
}

func (q *parseQueueFake) ClaimTask(context.Context) (ParseTaskLease, error) { return q.lease, nil }
func (q *parseQueueFake) CompleteTask(_ context.Context, _ ParseTaskLease, result contracts.ID) error {
	q.completed = result
	return nil
}
func (q *parseQueueFake) FailTask(_ context.Context, _ ParseTaskLease, safe contracts.PublicError) error {
	q.failed = safe
	return nil
}

func TestParseWorkerRejectsInvalidComposition(t *testing.T) {
	if err := (ParseWorker{}).RunOnce(context.Background()); !errors.Is(err, ErrParseWorker) {
		t.Fatal("invalid worker accepted", err)
	}
}

func TestDecodeParseTaskPayloadRejectsMalformedInput(t *testing.T) {
	if _, err := DecodeParseTaskPayload([]byte(`{"DocumentID":"doc"}`)); !errors.Is(err, ErrParseWorker) {
		t.Fatal("incomplete payload accepted", err)
	}
}

func TestParseWorkerFencesPreparationFailureWhenQueueSupportsIt(t *testing.T) {
	q := &parseQueueFake{lease: ParseTaskLease{JobID: "job", JobVersion: 1, Lease: contracts.Lease{JobID: "job", WorkerID: "worker", AttemptID: "attempt", FencingToken: 1, ExpiresAt: time.Now().Add(time.Minute)}, Payload: []byte(`{}`)}}
	w := ParseWorker{Queue: q, Decode: func([]byte) (DraftParseCommand, error) { return DraftParseCommand{}, errors.New("malformed") }}
	if err := w.RunOnce(context.Background()); err == nil || q.failed.Code != "dependency_unavailable" || q.failed.RequestID != "attempt" {
		t.Fatalf("err=%v failure=%#v", err, q.failed)
	}
}

func TestParseWorkerRuntimeOwnsLoopAndReadiness(t *testing.T) {
	q := &parseQueueFake{}
	w, err := NewParseWorkerRuntime(ParseWorker{Queue: q, Decode: func([]byte) (DraftParseCommand, error) {
		return DraftParseCommand{}, errors.New("no queued task")
	}}, time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	if w.Ready() == nil {
		t.Fatal("runtime was ready before start")
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := w.Ready(); err != nil {
		t.Fatal("started parser runtime was not ready", err)
	}
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	if w.Ready() == nil {
		t.Fatal("stopped parser runtime remained ready")
	}
	if err := w.Start(context.Background()); !errors.Is(err, ErrParseWorkerRuntime) {
		t.Fatal("stopped parser runtime restarted", err)
	}
}
