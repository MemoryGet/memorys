package preparation

import (
	"context"
	"math"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// Completion identifies an immutable prepared artifact, never a client-supplied
// assertion that a backend is ready. The store must inspect actual data again.
type CompletionRequest struct {
	Access             document.PublicationReadWorkRequest
	CommandID          contracts.ID
	ArtifactDigest     contracts.Digest
	ExpectedJobVersion uint64
}
type CompletionResult struct {
	Job         contracts.Job
	Artifact    Artifact
	CompletedAt time.Time
}
type CompletionStore interface {
	Complete(context.Context, CompletionRequest) (CompletionResult, error)
}

// CompleteJob is a pure transition. It proves neither current authority nor
// index readiness: a transactional adapter must supply both independently.
func CompleteJob(job contracts.Job, held contracts.Lease, r CompletionRequest, artifact Artifact, now time.Time) (contracts.Job, error) {
	g := artifact.GenerationSet.Channels[contracts.KeywordChannel]
	if job.State != "running" || job.RowVersion == 0 || job.RowVersion == math.MaxUint64 || job.RowVersion != r.ExpectedJobVersion || job.ID != artifact.JobID || job.OperationID != artifact.OperationID || job.Stage != "publication.prepare" || job.ProfileDigest != artifact.Profile.Digest || job.RevisionID == nil || *job.RevisionID != artifact.GenerationSet.RevisionID || job.FencingToken != artifact.FencingToken || artifact.FencingToken != r.Access.Lease.FencingToken || artifact.AttemptID != r.Access.Lease.AttemptID || !document.SamePublicationLease(held, r.Access.Lease) || now.IsZero() || !held.ExpiresAt.After(now) || job.LeaseUntil == nil || !job.LeaseUntil.Equal(held.ExpiresAt) || r.ArtifactDigest != artifact.Digest || !id(g.ID) || artifact.GenerationSet.State != "ready" {
		return contracts.Job{}, document.ErrPublicationTaskRejected
	}
	job.State = "completed"
	job.RowVersion++
	job.LeaseUntil = nil
	job.Progress = 1
	job.GenerationID = &g.ID
	job.Error = nil
	return own(job, 32768)
}
