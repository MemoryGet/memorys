package cleanup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type taskFixture struct {
	lease     contracts.CleanupLease
	claims    int
	outcome   string
	reference contracts.ID
}

func (t *taskFixture) Claim(context.Context, contracts.AccessScope, contracts.ID, contracts.ID, contracts.ID, time.Duration) (contracts.CleanupLease, error) {
	t.claims++
	return t.lease, nil
}
func (t *taskFixture) Finish(_ context.Context, _ contracts.AccessScope, _ contracts.CleanupLease, outcome string, ref contracts.ID) error {
	t.outcome, t.reference = outcome, ref
	return nil
}

type executorFixture struct {
	lookupState string
	executions  int
	observed    bool
	forge       bool
}

func (e *executorFixture) Lookup(_ context.Context, op contracts.CleanupOperation) (contracts.CleanupReceipt, error) {
	r := contracts.CleanupReceipt{OperationID: op.ID, RequestDigest: op.Digest, State: e.lookupState}
	if r.State == "succeeded" {
		r.ResultReference = "receipt"
	}
	return r, nil
}
func (e *executorFixture) Execute(_ context.Context, op contracts.CleanupOperation) (contracts.CleanupReceipt, error) {
	e.executions++
	if !e.forge {
		e.observed = true
	}
	return contracts.CleanupReceipt{OperationID: op.ID, RequestDigest: op.Digest, State: "succeeded", ResultReference: "receipt"}, nil
}

func fixture() (*taskFixture, *executorFixture, Worker, contracts.AccessScope) {
	g, b, ch := contracts.ID("generation"), contracts.ID("backend"), contracts.KeywordChannel
	lease := contracts.CleanupLease{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc", PlanID: "plan", TargetID: "target", WorkerID: "worker", RevisionID: "revision", GenerationSetID: "set", PublicationEpoch: 1, FencingToken: 1, Attempt: 1, RowVersion: 2, ExpiresAt: time.Now().Add(time.Minute), Target: contracts.CleanupTarget{ID: "target", Kind: "index", ModuleInstanceID: "engine", BindingReference: "binding", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), GenerationID: &g, BackendEpochID: &b, Channel: &ch}}
	tasks, executor := &taskFixture{lease: lease}, &executorFixture{lookupState: "absent"}
	worker := Worker{Tasks: tasks, VerifyLease: func(context.Context, contracts.AccessScope, contracts.CleanupLease) error { return nil }, Resolve: func(context.Context, contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
		return executor, nil
	}, AuthorizeExecution: func(context.Context, contracts.AccessScope, contracts.CleanupLease, contracts.CleanupOperation) error {
		return nil
	}, VerifyReceipt: func(_ context.Context, _ contracts.CleanupOperation, r contracts.CleanupReceipt) error {
		if r.State == "succeeded" && !executor.observed {
			return errors.New("actual effect missing")
		}
		return nil
	}}
	scope := contracts.AccessScope{ScopeID: "scope", SubjectID: "worker", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, Action: "publication.cleanup.execute", ExpiresAt: time.Now().Add(time.Minute)}
	return tasks, executor, worker, scope
}
func TestLookupReplayAndForgedSuccess(t *testing.T) {
	for _, replay := range []bool{true, false} {
		t.Run(map[bool]string{true: "replay", false: "forged"}[replay], func(t *testing.T) {
			tasks, executor, w, scope := fixture()
			executor.forge = !replay
			if replay {
				executor.lookupState = "succeeded"
				executor.observed = true
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := w.RunOne(ctx, scope, "plan", "target", "worker", time.Second)
			if replay {
				if err != nil || executor.executions != 0 || tasks.outcome != "succeeded" {
					t.Fatal("replay executed again", err)
				}
			} else {
				if !errors.Is(err, ErrUnknown) || tasks.outcome != "unknown" || tasks.reference != "" {
					t.Fatal("echo receipt accepted as effect", err)
				}
			}
		})
	}
}
func TestRevocationAfterAdmissionStopsExecute(t *testing.T) {
	tasks, executor, w, scope := fixture()
	calls := 0
	denied := errors.New("lease revoked")
	w.VerifyLease = func(context.Context, contracts.AccessScope, contracts.CleanupLease) error {
		calls++
		if calls == 3 {
			return denied
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := w.RunOne(ctx, scope, "plan", "target", "worker", time.Second)
	if !errors.Is(err, denied) || executor.executions != 0 || tasks.outcome != "" {
		t.Fatal("revocation ignored", err)
	}
}
func TestOperationStableAndAdapterCannotMutateSource(t *testing.T) {
	tasks, executor, w, scope := fixture()
	original, err := Operation(tasks.lease)
	if err != nil {
		t.Fatal(err)
	}
	retry := tasks.lease
	retry.WorkerID = "other"
	retry.FencingToken = 99
	retry.Attempt = 99
	retry.RowVersion = 99
	retry.ExpiresAt = time.Now().Add(time.Hour)
	op, err := Operation(retry)
	if err != nil || op.ID != original.ID || op.Digest != original.Digest {
		t.Fatal("attempt changed effect key")
	}
	w.Resolve = func(_ context.Context, op contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
		*op.Target.GenerationID = "other-generation"
		return executor, nil
	}
	w.VerifyReceipt = func(_ context.Context, op contracts.CleanupOperation, r contracts.CleanupReceipt) error {
		if *op.Target.GenerationID != "generation" || r.State == "succeeded" && !executor.observed {
			return errors.New("mutated source")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := w.RunOne(ctx, scope, "plan", "target", "worker", time.Second); err != nil || tasks.outcome != "succeeded" || *tasks.lease.Target.GenerationID != "generation" {
		t.Fatal("adapter changed frozen source", err)
	}
}
func TestUnknownAndMissingVerifierDoNotExecute(t *testing.T) {
	tasks, executor, w, scope := fixture()
	executor.lookupState = "pending"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := w.RunOne(ctx, scope, "plan", "target", "worker", time.Second); !errors.Is(err, ErrUnknown) || executor.executions != 0 || tasks.outcome != "unknown" {
		t.Fatal("pending effect redone", err)
	}
	tasks.claims = 0
	w.VerifyReceipt = nil
	if err := w.RunOne(ctx, scope, "plan", "target", "worker", time.Second); !errors.Is(err, ErrRejected) || tasks.claims != 0 {
		t.Fatal("missing verifier accepted")
	}
}
