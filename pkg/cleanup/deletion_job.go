package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type DeletionWorker struct {
	Jobs    contracts.DeletionCleanupStore
	Targets contracts.CleanupTargetRunner
}

// RunOne processes at most one fixed target. Lease ownership belongs to the
// target task; no aggregate lease is fabricated. Unknown effects are not retried.
func (w DeletionWorker) RunOne(ctx context.Context, input contracts.DeletionCleanupRequest) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || w.Jobs == nil || w.Targets == nil || input.Scope.Action != "publication.cleanup.execute" || input.Scope.SubjectID != input.WorkerID || len(input.Scope.KnowledgeBaseIDs) != 1 || !validID(input.JobID) || !validID(input.WorkerID) || input.Request.Deadline.IsZero() || input.TargetLeaseDuration < time.Millisecond || input.TargetLeaseDuration > 30*time.Second || input.TargetLeaseDuration%time.Millisecond != 0 || !validID(input.Scope.ScopeID) || !validID(input.Scope.SpaceID) || !validID(input.Scope.KnowledgeBaseIDs[0]) || input.Scope.PolicyVersion == 0 || !input.Scope.ExpiresAt.After(time.Now()) {
		return empty, ErrRejected
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrRejected
	}
	raw, e := json.Marshal(input)
	if e != nil {
		return empty, ErrRejected
	}
	var q contracts.DeletionCleanupRequest
	if json.Unmarshal(raw, &q) != nil {
		return empty, ErrRejected
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, q.Scope.ExpiresAt)
	defer expire()
	clone := func() contracts.DeletionCleanupRequest {
		raw, _ := json.Marshal(q)
		var copied contracts.DeletionCleanupRequest
		_ = json.Unmarshal(raw, &copied)
		return copied
	}
	selection, e := w.Jobs.NextDeletionCleanup(ctx, clone())
	if e != nil {
		return empty, e
	}
	if selection.Record.Job.ID != q.JobID || selection.Record.Job.SpaceID != q.Scope.SpaceID || !validID(selection.Record.ResourceID) {
		return empty, ErrRejected
	}
	var workError error
	if selection.HasTarget {
		if selection.Record.Job.State != "running" || !validID(selection.PlanID) || !validID(selection.TargetID) {
			return empty, ErrRejected
		}
		scope := q.Scope
		scope.KnowledgeBaseIDs = append([]contracts.ID(nil), q.Scope.KnowledgeBaseIDs...)
		workError = w.Targets.RunOne(ctx, scope, selection.PlanID, selection.TargetID, q.WorkerID, q.TargetLeaseDuration)
	}
	record, e := w.Jobs.ReconcileDeletionCleanup(ctx, clone())
	if e != nil {
		return empty, errors.Join(workError, e)
	}
	if record.Job.ID != q.JobID || record.ResourceID != selection.Record.ResourceID || record.Job.SpaceID != q.Scope.SpaceID {
		return empty, ErrRejected
	}
	return record, workError
}
