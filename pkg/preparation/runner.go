package preparation

import (
	"context"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

var ErrRunner = errors.New("publication worker step unavailable")
var ErrRunnerNoWork = errors.New("publication worker has no queued work")

// RunOnceRequest is one queue delivery. The queue owns discovery and retry;
// this step owns exactly one durable claim and never invents a new job ID.
type RunOnceRequest struct {
	Claim     document.PublicationClaimRequest
	CommandID contracts.ID
}

type RunOnceResult struct {
	Lease      contracts.Lease
	JobVersion uint64
	Artifact   Artifact
	Completed  CompletionResult
}

// Runner executes a bounded publication preparation step. On preparation or
// completion failure it returns the error without fabricating a failed receipt;
// the task store's lease expiry/fencing path remains authoritative for retry or
// takeover. Queue implementations may call RunOnce again with a fresh claim.
type Runner struct {
	Tasks    document.PublicationTaskStore
	Prepare  func(context.Context, document.PublicationReadWorkRequest) (Artifact, error)
	Complete CompletionStore
}

func NewRunner(worker Worker, complete CompletionStore) Runner {
	return Runner{Tasks: worker.Tasks, Prepare: worker.Prepare, Complete: complete}
}

// Run repeatedly asks the queue adapter for one delivery. The adapter owns
// discovery, ordering and retry policy; Runner owns bounded execution and
// invokes onError without leaking private backend details. A no-work result is
// a normal poll and uses the same bounded interval as failed polls.
func (r Runner) Run(ctx context.Context, next func(context.Context) (RunOnceRequest, error), interval time.Duration, onError func(error)) error {
	if ctx == nil || next == nil || interval < time.Millisecond || interval > time.Minute {
		return ErrRunner
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		request, err := next(ctx)
		if err == nil {
			_, err = r.RunOnce(ctx, request)
		}
		if err != nil && !errors.Is(err, ErrRunnerNoWork) && onError != nil {
			onError(err)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r Runner) RunOnce(ctx context.Context, input RunOnceRequest) (RunOnceResult, error) {
	var empty RunOnceResult
	if ctx == nil || r.Tasks == nil || r.Prepare == nil || r.Complete == nil || input.CommandID == "" || input.Claim.JobID == "" || input.Claim.WorkerID == "" || input.Claim.Request.RequestID == "" || input.Claim.Request.Deadline.IsZero() || input.Claim.Scope.Action != "publication.prepare" {
		return empty, ErrRunner
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrRunner
	}
	claimed, err := r.Tasks.Claim(ctx, input.Claim)
	if err != nil || claimed.Lease.JobID != input.Claim.JobID || claimed.Lease.WorkerID != input.Claim.WorkerID || claimed.Lease.AttemptID == "" || claimed.Lease.FencingToken == 0 || claimed.JobVersion == 0 {
		return empty, ErrRunner
	}
	access := document.PublicationReadWorkRequest{Request: input.Claim.Request, Scope: input.Claim.Scope, Lease: claimed.Lease}
	artifact, err := r.Prepare(ctx, access)
	if err != nil {
		return RunOnceResult{Lease: claimed.Lease, JobVersion: claimed.JobVersion}, err
	}
	if artifact.JobID != claimed.Lease.JobID || artifact.AttemptID != claimed.Lease.AttemptID || artifact.FencingToken != claimed.Lease.FencingToken || artifact.Digest == "" {
		return RunOnceResult{Lease: claimed.Lease, JobVersion: claimed.JobVersion}, ErrRunner
	}
	completed, err := r.Complete.Complete(ctx, CompletionRequest{Access: access, CommandID: input.CommandID, ArtifactDigest: artifact.Digest, ExpectedJobVersion: claimed.JobVersion})
	if err != nil {
		return RunOnceResult{Lease: claimed.Lease, JobVersion: claimed.JobVersion, Artifact: artifact}, err
	}
	if completed.Job.ID != claimed.Lease.JobID || completed.Job.State != "completed" || completed.Artifact.Digest != artifact.Digest {
		return RunOnceResult{Lease: claimed.Lease, JobVersion: claimed.JobVersion, Artifact: artifact, Completed: completed}, ErrRunner
	}
	return RunOnceResult{Lease: claimed.Lease, JobVersion: claimed.JobVersion, Artifact: artifact, Completed: completed}, nil
}
