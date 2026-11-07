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

type runnerTasks struct {
	lease   contracts.Lease
	version uint64
	claims  int
}

func (s *runnerTasks) Claim(_ context.Context, _ document.PublicationClaimRequest) (document.PublicationLeaseResult, error) {
	s.claims++
	return document.PublicationLeaseResult{Lease: s.lease, JobVersion: s.version}, nil
}
func (*runnerTasks) Renew(context.Context, document.PublicationRenewRequest) (document.PublicationLeaseResult, error) {
	return document.PublicationLeaseResult{}, errors.New("unused")
}
func (*runnerTasks) ReadOwned(context.Context, document.PublicationReadWorkRequest) (document.PublicationWork, error) {
	return document.PublicationWork{}, errors.New("unused")
}

type runnerCompletion struct {
	calls int
}

func (s *runnerCompletion) Complete(_ context.Context, in CompletionRequest) (CompletionResult, error) {
	s.calls++
	return CompletionResult{Job: contracts.Job{ID: in.Access.Lease.JobID, State: "completed"}, Artifact: Artifact{JobID: in.Access.Lease.JobID, AttemptID: in.Access.Lease.AttemptID, FencingToken: in.Access.Lease.FencingToken, Digest: in.ArtifactDigest}, CompletedAt: time.Now().UTC()}, nil
}

func TestRunnerClaimsPreparesAndCompletesOneBoundedStep(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	lease := contracts.Lease{JobID: "job", WorkerID: "worker", AttemptID: "attempt", FencingToken: 1, ExpiresAt: deadline}
	tasks := &runnerTasks{lease: lease, version: 2}
	completion := &runnerCompletion{}
	runner := Runner{Tasks: tasks, Complete: completion, Prepare: func(_ context.Context, access document.PublicationReadWorkRequest) (Artifact, error) {
		return Artifact{JobID: access.Lease.JobID, AttemptID: access.Lease.AttemptID, FencingToken: access.Lease.FencingToken, Digest: contracts.Digest(strings.Repeat("a", 64))}, nil
	}}
	claim := document.PublicationClaimRequest{ClaimRequest: contracts.ClaimRequest{Request: contracts.RequestContext{RequestID: "request", ProfileDigest: contracts.Digest(strings.Repeat("b", 64)), Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "worker", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "publication.prepare", PolicyVersion: 1, ExpiresAt: deadline}, JobID: "job", WorkerID: "worker", LeaseDuration: time.Second}}
	result, err := runner.RunOnce(ctx, RunOnceRequest{Claim: claim, CommandID: "command"})
	if err != nil || result.Completed.Job.State != "completed" || tasks.claims != 1 || completion.calls != 1 {
		t.Fatalf("runner step failed: %+v err=%v", result, err)
	}
}

func TestRunnerLeavesLeaseForRecoveryWhenPreparationFails(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tasks := &runnerTasks{lease: contracts.Lease{JobID: "job", WorkerID: "worker", AttemptID: "attempt", FencingToken: 1, ExpiresAt: deadline}, version: 2}
	completion := &runnerCompletion{}
	runner := Runner{Tasks: tasks, Complete: completion, Prepare: func(context.Context, document.PublicationReadWorkRequest) (Artifact, error) {
		return Artifact{}, errors.New("backend unavailable")
	}}
	claim := document.PublicationClaimRequest{ClaimRequest: contracts.ClaimRequest{Request: contracts.RequestContext{RequestID: "request", ProfileDigest: contracts.Digest(strings.Repeat("b", 64)), Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "worker", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "publication.prepare", PolicyVersion: 1, ExpiresAt: deadline}, JobID: "job", WorkerID: "worker", LeaseDuration: time.Second}}
	_, err := runner.RunOnce(ctx, RunOnceRequest{Claim: claim, CommandID: "command"})
	if err == nil || completion.calls != 0 || tasks.claims != 1 {
		t.Fatalf("failed preparation was incorrectly completed: err=%v claims=%d completes=%d", err, tasks.claims, completion.calls)
	}
}

func TestRunnerRunPollsNoWorkAndReportsOneFailureThenStops(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tasks := &runnerTasks{lease: contracts.Lease{JobID: "job", WorkerID: "worker", AttemptID: "attempt", FencingToken: 1, ExpiresAt: deadline}, version: 2}
	runner := Runner{Tasks: tasks, Complete: &runnerCompletion{}, Prepare: func(context.Context, document.PublicationReadWorkRequest) (Artifact, error) {
		return Artifact{}, errors.New("temporary")
	}}
	claim := document.PublicationClaimRequest{ClaimRequest: contracts.ClaimRequest{Request: contracts.RequestContext{RequestID: "request", ProfileDigest: contracts.Digest(strings.Repeat("b", 64)), Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "worker", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "publication.prepare", PolicyVersion: 1, ExpiresAt: deadline}, JobID: "job", WorkerID: "worker", LeaseDuration: time.Second}}
	calls, failures := 0, 0
	err := runner.Run(ctx, func(context.Context) (RunOnceRequest, error) {
		calls++
		if calls == 1 {
			return RunOnceRequest{}, ErrRunnerNoWork
		}
		return RunOnceRequest{Claim: claim, CommandID: "command"}, nil
	}, time.Millisecond, func(error) {
		failures++
		cancel()
	})
	if !errors.Is(err, context.Canceled) || calls < 2 || failures != 1 {
		t.Fatalf("runner loop did not stop after reported failure: err=%v calls=%d failures=%d", err, calls, failures)
	}
}
