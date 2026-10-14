package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

// The external executor and its effect check are fixtures; only task storage,
// fixed-plan reads and worker orchestration execute real application code.
type cleanupExecutorFixture struct{ applied bool }

func (e *cleanupExecutorFixture) Lookup(_ context.Context, op contracts.CleanupOperation) (contracts.CleanupReceipt, error) {
	state := "absent"
	ref := contracts.ID("")
	if e.applied {
		state = "succeeded"
		ref = "worker-receipt"
	}
	return contracts.CleanupReceipt{OperationID: op.ID, RequestDigest: op.Digest, State: state, ResultReference: ref}, nil
}
func (e *cleanupExecutorFixture) Execute(ctx context.Context, op contracts.CleanupOperation) (contracts.CleanupReceipt, error) {
	e.applied = true
	return e.Lookup(ctx, op)
}

func verifyCleanupWorker(ctx context.Context, db *sql.DB, scope contracts.AccessScope) error {
	scope.SubjectID = "worker-a"
	scope.Action = "publication.cleanup.execute"
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "worker-a" || s.Action != "publication.cleanup.execute" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("fixture worker scope denied")
		}
		return nil
	}
	store := postgres.CleanupTasks{DB: db, Verify: verify}
	executor := &cleanupExecutorFixture{}
	w := cleanup.Worker{Tasks: store, VerifyLease: store.VerifyLease, Resolve: func(_ context.Context, op contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
		if op.Target.BindingReference != "source-cache" || op.PublicationEpoch != 1 {
			return nil, errors.New("wrong source binding")
		}
		return executor, nil
	}, AuthorizeExecution: func(context.Context, contracts.AccessScope, contracts.CleanupLease, contracts.CleanupOperation) error {
		return nil
	}, VerifyReceipt: func(_ context.Context, _ contracts.CleanupOperation, r contracts.CleanupReceipt) error {
		if r.State == "succeeded" && !executor.applied {
			return errors.New("fixture effect missing")
		}
		return nil
	}}
	if err := w.RunOne(ctx, scope, "plan-success", "cache-target", "worker-a", 2*time.Second); err != nil {
		return err
	}
	var state, reference string
	if db.QueryRowContext(ctx, `SELECT state,result_reference FROM akos_withdraw_v1.cleanup_progress WHERE plan_id='plan-success' AND target_id='cache-target'`).Scan(&state, &reference) != nil || state != "succeeded" || reference != "worker-receipt" || !executor.applied {
		return errors.New("worker result did not persist")
	}
	return nil
}
