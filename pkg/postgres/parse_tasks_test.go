package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestParseTaskRepositoryRejectsInvalidControlInput(t *testing.T) {
	repo := ParseTaskRepository{}
	if _, err := repo.Read(context.Background(), "space", "kb", "task"); !errors.Is(err, ErrParseTaskRepository) {
		t.Fatal("invalid read accepted", err)
	}
	if _, err := repo.Cancel(context.Background(), "space", "kb", "task", "actor", "request", 0); !errors.Is(err, ErrParseTaskRepository) {
		t.Fatal("zero version accepted", err)
	}
	if _, err := repo.Fail(context.Background(), "space", "kb", contracts.Lease{JobID: "task", WorkerID: "worker", AttemptID: "attempt", FencingToken: 1}, 1, contracts.PublicError{}); !errors.Is(err, ErrParseTaskRepository) {
		t.Fatal("invalid failure accepted", err)
	}
}

func TestParseTaskLeaseExpired(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Minute)
	if parseTaskLeaseExpired(contracts.Job{LeaseUntil: &expires}, now) {
		t.Fatal("future boundary incorrectly expired")
	}
}
