package cleanup_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type deletionQueue func(context.Context, contracts.DeletionQueueRequest) (contracts.DeletionQueuePage, error)

func (f deletionQueue) DiscoverDeletionCleanup(c context.Context, q contracts.DeletionQueueRequest) (contracts.DeletionQueuePage, error) {
	return f(c, q)
}

type deletionRunner func(context.Context, contracts.DeletionCleanupRequest) (contracts.ResourceJob, error)

func (f deletionRunner) RunOne(c context.Context, q contracts.DeletionCleanupRequest) (contracts.ResourceJob, error) {
	return f(c, q)
}

func pollScope(policy uint64) contracts.AccessScope {
	return contracts.AccessScope{ScopeID: "scope", SubjectID: "worker", Action: "publication.cleanup.execute", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: policy, ExpiresAt: time.Now().Add(time.Minute)}
}
func pollConfig(queue contracts.DeletionCleanupQueue, runner contracts.DeletionJobRunner, scope func(context.Context) (contracts.AccessScope, error)) cleanup.DeletionPollConfig {
	return cleanup.DeletionPollConfig{Queue: queue, Runner: runner, Scope: scope, WorkerID: "worker", SpaceID: "space", KnowledgeBaseID: "kb", PageSize: 1, MaxPages: 1, CycleBudget: time.Second, PollInterval: time.Millisecond, TargetLeaseDuration: time.Second}
}

func TestDeletionPollerPreservesFairPositionAndResetsChangedAuthority(t *testing.T) {
	policy := uint64(1)
	scopes := 0
	var ran []contracts.ID
	queue := deletionQueue(func(_ context.Context, q contracts.DeletionQueueRequest) (contracts.DeletionQueuePage, error) {
		binding, e := cleanup.QueueBinding(q.Scope, q.WorkerID, q.Limit)
		if e != nil {
			return contracts.DeletionQueuePage{}, e
		}
		after := contracts.ID("")
		if q.Cursor != nil {
			if q.Cursor.Binding != binding {
				return contracts.DeletionQueuePage{}, errors.New("stale binding")
			}
			after = q.Cursor.AfterJobID
		}
		id := contracts.ID("a")
		if after == "a" {
			id = "b"
		}
		if after == "b" {
			id = "c"
		}
		q.Scope.KnowledgeBaseIDs[0] = "provider-mutated"
		page := contracts.DeletionQueuePage{UpperJobID: "c", Items: []contracts.DeletionCleanupTicket{{JobID: id, ResourceID: "doc", SpaceID: "space", KnowledgeBaseID: "kb", ProfileDigest: contracts.Digest(strings.Repeat("a", 64))}}}
		if id != "c" {
			page.Next = &contracts.DeletionQueueCursor{AfterJobID: id, UpperJobID: "c", Binding: binding}
		}
		return page, nil
	})
	runner := deletionRunner(func(_ context.Context, q contracts.DeletionCleanupRequest) (contracts.ResourceJob, error) {
		if q.Scope.KnowledgeBaseIDs[0] != "kb" {
			t.Fatal("discovery provider rewrote execution scope")
		}
		ran = append(ran, q.JobID)
		return contracts.ResourceJob{ResourceID: "doc", Job: contracts.Job{ID: q.JobID, SpaceID: "space", State: "running", ProfileDigest: q.Request.ProfileDigest}}, nil
	})
	poller, e := cleanup.NewDeletionPoller(pollConfig(queue, runner, func(context.Context) (contracts.AccessScope, error) {
		scopes++
		scope := pollScope(policy)
		scope.ScopeID = contracts.ID("scope-" + strconv.Itoa(scopes))
		return scope, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		if _, e = poller.RunOnce(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	// One page per cycle still reaches c, then wraps to a. New scope IDs did not
	// restart every cycle and starve later jobs.
	if strings.Join([]string{string(ran[0]), string(ran[1]), string(ran[2]), string(ran[3])}, "") != "abca" {
		t.Fatal(ran)
	}
	policy = 2
	if _, e = poller.RunOnce(context.Background()); e != nil || ran[4] != "a" {
		t.Fatal("authority change reused old cursor", e, ran)
	}
}

func TestDeletionPollerRejectsWholeInvalidPageBeforeEffects(t *testing.T) {
	calls := 0
	queue := deletionQueue(func(_ context.Context, q contracts.DeletionQueueRequest) (contracts.DeletionQueuePage, error) {
		return contracts.DeletionQueuePage{UpperJobID: "b", Items: []contracts.DeletionCleanupTicket{{JobID: "a", ResourceID: "doc", SpaceID: "space", KnowledgeBaseID: "kb", ProfileDigest: contracts.Digest(strings.Repeat("a", 64))}, {JobID: "b", ResourceID: "foreign", SpaceID: "space", KnowledgeBaseID: "other-kb", ProfileDigest: contracts.Digest(strings.Repeat("a", 64))}}}, nil
	})
	runner := deletionRunner(func(context.Context, contracts.DeletionCleanupRequest) (contracts.ResourceJob, error) {
		calls++
		return contracts.ResourceJob{}, nil
	})
	config := pollConfig(queue, runner, func(context.Context) (contracts.AccessScope, error) { return pollScope(1), nil })
	config.PageSize = 2
	p, e := cleanup.NewDeletionPoller(config)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.RunOnce(context.Background()); e == nil || calls != 0 {
		t.Fatal("foreign page reached worker")
	}
}

func TestDeletionPollerWaitsAndStopsWithCaller(t *testing.T) {
	cycles, grants := 0, 0
	config := pollConfig(deletionQueue(func(context.Context, contracts.DeletionQueueRequest) (contracts.DeletionQueuePage, error) {
		return contracts.DeletionQueuePage{}, nil
	}), deletionRunner(func(context.Context, contracts.DeletionCleanupRequest) (contracts.ResourceJob, error) {
		t.Fatal("empty queue executed work")
		return contracts.ResourceJob{}, nil
	}), func(context.Context) (contracts.AccessScope, error) { grants++; return pollScope(1), nil })
	p, e := cleanup.NewDeletionPoller(config)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	e = p.Run(ctx, func(cleanup.DeletionPollReport) {
		cycles++
		if cycles == 2 {
			cancel()
		}
	})
	if !errors.Is(e, context.Canceled) || cycles != 2 || grants != 2 || time.Since(started) < config.PollInterval {
		t.Fatal("loop did not wait/renew/stop", e, cycles, grants)
	}
}

func TestDeletionPollerFailsClosedAfterDiscoveryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config := pollConfig(deletionQueue(func(context.Context, contracts.DeletionQueueRequest) (contracts.DeletionQueuePage, error) {
		cancel()
		return contracts.DeletionQueuePage{}, nil
	}), deletionRunner(func(context.Context, contracts.DeletionCleanupRequest) (contracts.ResourceJob, error) {
		t.Fatal("canceled discovery executed work")
		return contracts.ResourceJob{}, nil
	}), func(context.Context) (contracts.AccessScope, error) { return pollScope(1), nil })
	p, e := cleanup.NewDeletionPoller(config)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.RunOnce(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal("canceled empty discovery reported success", e)
	}
}

func TestDeletionPollerIsolatesKnownJobFailureAndStopsUnknownFailure(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(strconv.FormatBool(unknown), func(t *testing.T) {
			var ran []contracts.ID
			fault := errors.New("backend unavailable")
			queue := deletionQueue(func(context.Context, contracts.DeletionQueueRequest) (contracts.DeletionQueuePage, error) {
				p := contracts.DeletionQueuePage{UpperJobID: "b"}
				for _, id := range []contracts.ID{"a", "b"} {
					p.Items = append(p.Items, contracts.DeletionCleanupTicket{JobID: id, ResourceID: "doc", SpaceID: "space", KnowledgeBaseID: "kb", ProfileDigest: contracts.Digest(strings.Repeat("a", 64))})
				}
				return p, nil
			})
			runner := deletionRunner(func(_ context.Context, q contracts.DeletionCleanupRequest) (contracts.ResourceJob, error) {
				ran = append(ran, q.JobID)
				if q.JobID == "a" && unknown {
					return contracts.ResourceJob{}, fault
				}
				r := contracts.ResourceJob{ResourceID: "doc", Job: contracts.Job{ID: q.JobID, SpaceID: "space", ProfileDigest: q.Request.ProfileDigest, State: "completed"}}
				if q.JobID == "a" {
					r.Job.State = "failed"
					return r, fault
				}
				return r, nil
			})
			config := pollConfig(queue, runner, func(context.Context) (contracts.AccessScope, error) { return pollScope(1), nil })
			config.PageSize = 2
			p, e := cleanup.NewDeletionPoller(config)
			if e != nil {
				t.Fatal(e)
			}
			r, e := p.RunOnce(context.Background())
			if unknown {
				if !errors.Is(e, fault) || len(ran) != 1 {
					t.Fatal("unknown failure did not stop loop", e, ran)
				}
			} else if e != nil || len(ran) != 2 || r.Failed != 1 || r.Completed != 1 || r.WorkErrors != 1 {
				t.Fatal("known failed job starved next job", e, r, ran)
			}
		})
	}
}
