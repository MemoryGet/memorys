package document

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrPublicationTaskRejected = errors.New("publication task unavailable or stale")
var ErrPublicationTaskUnknown = errors.New("publication task mutation requires same-request reconciliation")
var ErrPublicationSuperseded = errors.New("publication preparation plan is superseded")

type PublicationTaskAccess struct {
	Kind            string
	Request         contracts.RequestContext
	Scope           contracts.AccessScope
	JobID, WorkerID contracts.ID
	Lease           contracts.Lease
	Duration        time.Duration
	CommandID       contracts.ID
}

type PublicationClaimRequest struct {
	contracts.ClaimRequest
	CommandID contracts.ID
}

type PublicationLeaseResult struct {
	Lease      contracts.Lease
	JobVersion uint64
}
type PublicationRenewRequest struct {
	Request   contracts.RequestContext
	Scope     contracts.AccessScope
	Lease     contracts.Lease
	Duration  time.Duration
	CommandID contracts.ID
}
type PublicationReadWorkRequest struct {
	Request contracts.RequestContext
	Scope   contracts.AccessScope
	Lease   contracts.Lease
}
type PublicationWork struct {
	Plan          PublicationStartPlan
	Job           contracts.Job
	Lease         contracts.Lease
	Revision      contracts.Revision
	FrozenPayload []byte
}

// Receipts are historical responses, not current execution authority. ReadOwned
// must validate the exact current lease and original publisher before releasing
// any content. These ports are not the complete TaskRepository/TaskQueue.
type PublicationTaskStore interface {
	Claim(context.Context, PublicationClaimRequest) (PublicationLeaseResult, error)
	Renew(context.Context, PublicationRenewRequest) (PublicationLeaseResult, error)
	ReadOwned(context.Context, PublicationReadWorkRequest) (PublicationWork, error)
}

func SamePublicationLease(a, b contracts.Lease) bool {
	return a.JobID == b.JobID && a.WorkerID == b.WorkerID && a.AttemptID == b.AttemptID && a.FencingToken == b.FencingToken && a.ExpiresAt.Equal(b.ExpiresAt)
}

// TransitionPublicationLease uses the database's now, not a worker wall clock.
// The adapter must lock/CAS the supplied job, persist the receipt and audit, and
// never accept this pure calculation as proof of current authority.
func TransitionPublicationLease(job contracts.Job, held, requested contracts.Lease, worker, attemptID contracts.ID, kind string, duration time.Duration, now, deadline time.Time) (contracts.Job, contracts.Lease, error) {
	if !frozenID(job.ID) || !frozenID(job.SpaceID) || !frozenID(job.OperationID) || job.RevisionID == nil || !frozenID(*job.RevisionID) || job.Stage != "publication.prepare" || !candidateDigest(job.ProfileDigest) || job.RowVersion == 0 || job.RowVersion == math.MaxUint64 || !frozenID(worker) || duration < time.Millisecond || duration > 30*time.Second || duration%time.Millisecond != 0 || now.IsZero() || !deadline.After(now) {
		return contracts.Job{}, contracts.Lease{}, ErrPublicationTaskRejected
	}
	expires := now.Add(duration)
	if deadline.Before(expires) {
		expires = deadline
	}
	expires = expires.UTC()
	if !expires.After(now) || expires.Year() < 1 || expires.Year() > 9999 {
		return contracts.Job{}, contracts.Lease{}, ErrPublicationTaskRejected
	}
	if job.State == "running" {
		if job.LeaseUntil == nil || held.JobID != job.ID || held.FencingToken != job.FencingToken || !held.ExpiresAt.Equal(*job.LeaseUntil) || !frozenID(held.WorkerID) || !frozenID(held.AttemptID) || job.Attempt == 0 || job.FencingToken == 0 {
			return contracts.Job{}, contracts.Lease{}, ErrPublicationTaskRejected
		}
	}
	var lease contracts.Lease
	switch kind {
	case "claim":
		if !frozenID(attemptID) || job.Attempt == math.MaxUint64 || job.FencingToken == math.MaxUint64 || job.State != "queued" && job.State != "running" || job.State == "running" && held.ExpiresAt.After(now) || job.State == "queued" && (job.Attempt != 0 || job.FencingToken != 0 || job.LeaseUntil != nil) {
			return contracts.Job{}, contracts.Lease{}, ErrPublicationTaskRejected
		}
		job.Attempt++
		job.FencingToken++
		job.State = "running"
		lease = contracts.Lease{JobID: job.ID, WorkerID: worker, AttemptID: attemptID, FencingToken: job.FencingToken, ExpiresAt: expires}
	case "renew":
		if job.State != "running" || held.WorkerID != worker || !SamePublicationLease(held, requested) || !held.ExpiresAt.After(now) || !expires.After(held.ExpiresAt) {
			return contracts.Job{}, contracts.Lease{}, ErrPublicationTaskRejected
		}
		lease = held
		lease.ExpiresAt = expires
	default:
		return contracts.Job{}, contracts.Lease{}, ErrPublicationTaskRejected
	}
	job.RowVersion++
	job.LeaseUntil = &expires
	raw, err := json.Marshal(job)
	if err != nil {
		return contracts.Job{}, contracts.Lease{}, ErrPublicationTaskRejected
	}
	var owned contracts.Job
	if json.Unmarshal(raw, &owned) != nil {
		return contracts.Job{}, contracts.Lease{}, ErrPublicationTaskRejected
	}
	return owned, lease, nil
}

func SupersedePublicationJob(job contracts.Job, requestID contracts.ID) (contracts.Job, error) {
	if job.RowVersion == 0 || job.RowVersion == math.MaxUint64 || job.FencingToken == math.MaxUint64 || job.State != "queued" && job.State != "running" {
		return contracts.Job{}, ErrPublicationTaskRejected
	}
	job.State = "failed"
	job.RowVersion++
	job.FencingToken++
	job.LeaseUntil = nil
	job.Error = &contracts.PublicError{Code: "conflict", SafeMessage: "publication preparation superseded", RequestID: requestID, Retryable: false}
	raw, err := json.Marshal(job)
	if err != nil {
		return contracts.Job{}, ErrPublicationTaskRejected
	}
	var owned contracts.Job
	if json.Unmarshal(raw, &owned) != nil {
		return contracts.Job{}, ErrPublicationTaskRejected
	}
	return owned, nil
}
