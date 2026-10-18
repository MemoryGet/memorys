package cleanup_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type deleteJobStore struct {
	run   bool
	calls int
}

func (s *deleteJobStore) NextDeletionCleanup(_ context.Context, q contracts.DeletionCleanupRequest) (contracts.DeletionCleanupSelection, error) {
	s.calls++
	q.Scope.KnowledgeBaseIDs[0] = "provider-mutated"
	record := contracts.ResourceJob{ResourceID: "doc", Job: contracts.Job{ID: "job", SpaceID: "space", State: "running"}}
	if !s.run {
		record.Job.State = "completed"
	}
	return contracts.DeletionCleanupSelection{Record: record, PlanID: "plan", TargetID: "target", HasTarget: s.run}, nil
}
func (s *deleteJobStore) ReconcileDeletionCleanup(_ context.Context, q contracts.DeletionCleanupRequest) (contracts.ResourceJob, error) {
	s.calls++
	if q.Scope.KnowledgeBaseIDs[0] != "kb" {
		return contracts.ResourceJob{}, errors.New("provider changed reconciliation scope")
	}
	return contracts.ResourceJob{ResourceID: "doc", Job: contracts.Job{ID: "job", SpaceID: "space", State: "completed"}}, nil
}

type deleteTargetRunner func(context.Context, contracts.AccessScope, contracts.ID, contracts.ID, contracts.ID, time.Duration) error

func (f deleteTargetRunner) RunOne(c context.Context, s contracts.AccessScope, p, t, w contracts.ID, d time.Duration) error {
	return f(c, s, p, t, w, d)
}

func TestDeletionWorkerRunsOneFixedTargetAndReconcilesWithoutBlindRetry(t *testing.T) {
	for _, mode := range []string{"work", "completed", "unknown"} {
		store := &deleteJobStore{run: mode != "completed"}
		calls := 0
		runner := deleteTargetRunner(func(_ context.Context, s contracts.AccessScope, p, t, w contracts.ID, d time.Duration) error {
			calls++
			if s.KnowledgeBaseIDs[0] != "kb" || p != "plan" || t != "target" || w != "worker" || d != time.Second {
				return errors.New("target binding changed")
			}
			s.KnowledgeBaseIDs[0] = "runner-mutated"
			if mode == "unknown" {
				return cleanup.ErrUnknown
			}
			return nil
		})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		scope := contracts.AccessScope{ScopeID: "scope", SubjectID: "worker", Action: "publication.cleanup.execute", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}
		result, e := (cleanup.DeletionWorker{Jobs: store, Targets: runner}).RunOne(ctx, contracts.DeletionCleanupRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, Scope: scope, JobID: "job", WorkerID: "worker", TargetLeaseDuration: time.Second})
		cancel()
		expected := 1
		if mode == "completed" {
			expected = 0
		}
		if result.Job.ID != "job" || store.calls != 2 || calls != expected || scope.KnowledgeBaseIDs[0] != "kb" {
			t.Fatal("job orchestration/ownership failed", mode, e)
		}
		if mode == "unknown" {
			if !errors.Is(e, cleanup.ErrUnknown) {
				t.Fatal("unknown result hidden")
			}
		} else if e != nil {
			t.Fatal(e)
		}
	}
}
