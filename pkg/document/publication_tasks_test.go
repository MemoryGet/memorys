package document_test

import (
	"math"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func queuedPublicationJob(t *testing.T) (contracts.Job, time.Time) {
	t.Helper()
	r, d, v, p, b := publicationFixture(t)
	at := time.Now().UTC()
	plan, err := document.PreparePublicationStart(r, d, v, p, 1, b, 1, "operation", "job", at)
	if err != nil {
		t.Fatal(err)
	}
	return plan.Result.Job, at
}
func TestPublicationLeaseTakeoverChangesAttemptAndFencesOldOwner(t *testing.T) {
	job, now := queuedPublicationJob(t)
	running, first, err := document.TransitionPublicationLease(job, contracts.Lease{}, contracts.Lease{}, "worker-a", "attempt-a", "claim", time.Second, now, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if running.Attempt != 1 || running.FencingToken != 1 || running.RowVersion != 2 || running.State != "running" || job.State != "queued" {
		t.Fatal("claim mutated input or lost counters")
	}
	if _, _, err := document.TransitionPublicationLease(running, first, contracts.Lease{}, "worker-b", "attempt-b", "claim", time.Second, now.Add(time.Millisecond), now.Add(3*time.Second)); err == nil {
		t.Fatal("live owner displaced")
	}
	next, second, err := document.TransitionPublicationLease(running, first, contracts.Lease{}, "worker-b", "attempt-b", "claim", time.Second, now.Add(time.Second), now.Add(3*time.Second))
	if err != nil || next.Attempt != 2 || next.FencingToken != 2 || second.AttemptID == first.AttemptID {
		t.Fatal("takeover did not isolate attempts", err)
	}
	if _, _, err := document.TransitionPublicationLease(next, second, first, "worker-a", "", "renew", time.Second, now.Add(time.Second), now.Add(3*time.Second)); err == nil {
		t.Fatal("old owner renewed new attempt")
	}
}
func TestPublicationRenewalPreservesFenceAndClipsBudget(t *testing.T) {
	job, now := queuedPublicationJob(t)
	running, lease, err := document.TransitionPublicationLease(job, contracts.Lease{}, contracts.Lease{}, "worker", "attempt", "claim", time.Second, now, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(1500 * time.Millisecond)
	renewed, next, err := document.TransitionPublicationLease(running, lease, lease, "worker", "", "renew", 2*time.Second, now.Add(100*time.Millisecond), deadline)
	if err != nil || !next.ExpiresAt.Equal(deadline) || next.FencingToken != lease.FencingToken || next.AttemptID != lease.AttemptID || renewed.RowVersion != 3 || running.RowVersion != 2 {
		t.Fatal("renewal extended budget or changed ownership", err)
	}
	*renewed.RevisionID = "changed"
	if *running.RevisionID == "changed" {
		t.Fatal("returned job metadata aliases input")
	}
	if _, _, err := document.TransitionPublicationLease(running, lease, lease, "worker", "", "renew", time.Second, lease.ExpiresAt, deadline); err == nil {
		t.Fatal("expired lease renewed")
	}
}
func TestPublicationLeaseOverflowAndSupersededFence(t *testing.T) {
	job, now := queuedPublicationJob(t)
	job.RowVersion = math.MaxUint64
	if _, _, err := document.TransitionPublicationLease(job, contracts.Lease{}, contracts.Lease{}, "worker", "attempt", "claim", time.Second, now, now.Add(time.Second)); err == nil {
		t.Fatal("row version overflow")
	}
	job.RowVersion = 1
	failed, err := document.SupersedePublicationJob(job, "request")
	if err != nil || failed.State != "failed" || failed.LeaseUntil != nil || failed.FencingToken != 1 || failed.Error == nil || failed.Error.Retryable {
		t.Fatal("superseded plan not fenced", err)
	}
	if _, _, err := document.TransitionPublicationLease(failed, contracts.Lease{}, contracts.Lease{}, "worker", "attempt", "claim", time.Second, now, now.Add(time.Second)); err == nil {
		t.Fatal("terminal preparation revived")
	}
}
