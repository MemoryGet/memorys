package preparation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func serviceRunner() Runner {
	return Runner{
		Tasks: &runnerTasks{lease: contracts.Lease{JobID: "job", WorkerID: "worker", AttemptID: "attempt", FencingToken: 1, ExpiresAt: time.Now().Add(time.Minute)}, version: 2},
		Prepare: func(_ context.Context, access document.PublicationReadWorkRequest) (Artifact, error) {
			return Artifact{JobID: access.Lease.JobID, AttemptID: access.Lease.AttemptID, FencingToken: access.Lease.FencingToken, Digest: contracts.Digest(strings.Repeat("a", 64))}, nil
		},
		Complete: &runnerCompletion{},
	}
}

func TestServiceStartsReportsReadyAndStops(t *testing.T) {
	service, err := NewService(serviceRunner(), func(context.Context) (RunOnceRequest, error) { return RunOnceRequest{}, ErrRunnerNoWork }, time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	if service.Ready() == nil {
		t.Fatal("service ready before start")
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.Ready(); err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); !errors.Is(err, ErrWorkerService) {
		t.Fatal("service started twice", err)
	}
	if err := service.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := service.Ready(); !errors.Is(err, ErrWorkerService) {
		t.Fatal("stopped service remained ready")
	}
	if err := service.Stop(); err != nil {
		t.Fatal("stop should be idempotent", err)
	}
}

func TestServiceRejectsInvalidComposition(t *testing.T) {
	if _, err := NewService(Runner{}, nil, time.Second, nil); !errors.Is(err, ErrWorkerService) {
		t.Fatal("invalid service accepted", err)
	}
}
