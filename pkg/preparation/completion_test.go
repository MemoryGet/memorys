package preparation_test

import (
	"math"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

func TestCompletionBindsVersionArtifactAndExactLease(t *testing.T) {
	w, p, chunks := fixture(t)
	w.Job.Stage = "publication.prepare"
	w.Job.RevisionID = &w.Revision.ID
	w.Job.LeaseUntil = &w.Lease.ExpiresAt
	a, _, err := preparation.Build(w, p, chunks)
	if err != nil {
		t.Fatal(err)
	}
	r := preparation.CompletionRequest{CommandID: "complete", ArtifactDigest: a.Digest, ExpectedJobVersion: w.Job.RowVersion}
	r.Access.Lease = w.Lease
	now := time.Now()
	job, err := preparation.CompleteJob(w.Job, w.Lease, r, a, now)
	if err != nil || job.State != "completed" || job.Progress != 1 || job.RowVersion != w.Job.RowVersion+1 || job.GenerationID == nil || *job.GenerationID != a.GenerationSet.Channels[contracts.KeywordChannel].ID || job.LeaseUntil != nil || job.FencingToken != w.Job.FencingToken {
		t.Fatal("invalid completion", err)
	}
	*job.RevisionID = "caller-mutated"
	if *w.Job.RevisionID != w.Revision.ID {
		t.Fatal("completion aliased original job")
	}
	cases := map[string]func(*preparation.CompletionRequest){"version": func(r *preparation.CompletionRequest) { r.ExpectedJobVersion++ }, "digest": func(r *preparation.CompletionRequest) { r.ArtifactDigest = "wrong" }, "attempt": func(r *preparation.CompletionRequest) { r.Access.Lease.AttemptID = "other" }, "deadline": func(r *preparation.CompletionRequest) {
		r.Access.Lease.ExpiresAt = r.Access.Lease.ExpiresAt.Add(time.Nanosecond)
	}, "fence": func(r *preparation.CompletionRequest) { r.Access.Lease.FencingToken++ }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := r
			mutate(&bad)
			if _, err := preparation.CompleteJob(w.Job, w.Lease, bad, a, now); err == nil {
				t.Fatal("mismatched completion accepted")
			}
		})
	}
	if _, err := preparation.CompleteJob(w.Job, w.Lease, r, a, w.Lease.ExpiresAt); err == nil {
		t.Fatal("expired completion accepted")
	}
	w.Job.RowVersion = math.MaxUint64
	r.ExpectedJobVersion = w.Job.RowVersion
	if _, err := preparation.CompleteJob(w.Job, w.Lease, r, a, now); err == nil {
		t.Fatal("version overflow accepted")
	}
}
