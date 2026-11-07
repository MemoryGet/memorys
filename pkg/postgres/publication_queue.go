package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

var ErrPublicationQueue = errors.New("publication queue unavailable")

// PublicationQueue discovers durable queued preparation jobs. Claiming and
// fencing remain in PublicationTasks; this adapter never marks work running
// while merely discovering it.
type PublicationQueue struct {
	DB            *sql.DB
	Scope         func(context.Context) (contracts.AccessScope, error)
	Space         contracts.ID
	KnowledgeBase contracts.ID
	WorkerID      contracts.ID
	ProfileDigest contracts.Digest
	LeaseDuration time.Duration
	RequestBudget time.Duration
}

func (q PublicationQueue) Next(ctx context.Context) (preparation.RunOnceRequest, error) {
	var empty preparation.RunOnceRequest
	if ctx == nil || q.DB == nil || q.Scope == nil || q.Space == "" || q.KnowledgeBase == "" || q.WorkerID == "" || len(q.ProfileDigest) != 64 || q.LeaseDuration < time.Millisecond || q.LeaseDuration > 30*time.Second {
		return empty, ErrPublicationQueue
	}
	budget := q.RequestBudget
	if budget == 0 {
		budget = 30 * time.Second
	}
	if budget < time.Second || budget > 2*time.Minute {
		return empty, ErrPublicationQueue
	}
	var id, raw string
	err := q.DB.QueryRowContext(ctx, `SELECT id,payload FROM akos_withdraw_v1.publication_task_heads WHERE space_id=$1::text AND kb_id=$2::text AND state='queued' ORDER BY id LIMIT 1`, string(q.Space), string(q.KnowledgeBase)).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, preparation.ErrRunnerNoWork
	}
	if err != nil || len(raw) == 0 || len(raw) > 32768 {
		return empty, ErrPublicationQueue
	}
	var job contracts.Job
	if json.Unmarshal([]byte(raw), &job) != nil || job.ID != contracts.ID(id) || job.SpaceID != q.Space || job.State != "queued" || job.Stage != "publication.prepare" || job.RowVersion == 0 || job.ProfileDigest != q.ProfileDigest || job.RevisionID == nil || *job.RevisionID == "" || job.OperationID == "" {
		return empty, ErrPublicationQueue
	}
	scope, err := q.Scope(ctx)
	if err != nil || scope.Action != "publication.prepare" || scope.SubjectID != q.WorkerID || scope.SpaceID != q.Space || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != q.KnowledgeBase || scope.PolicyVersion == 0 || !scope.ExpiresAt.After(time.Now()) {
		return empty, ErrPublicationQueue
	}
	commandID := publicationQueueCommandID("claim", job.ID, q.WorkerID, job.RowVersion)
	requestID := publicationQueueCommandID("request", job.ID, q.WorkerID, job.RowVersion)
	deadline := time.Now().Add(budget)
	if scope.ExpiresAt.Before(deadline) {
		deadline = scope.ExpiresAt
	}
	if !deadline.After(time.Now()) {
		return empty, ErrPublicationQueue
	}
	return preparation.RunOnceRequest{Claim: document.PublicationClaimRequest{ClaimRequest: contracts.ClaimRequest{Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: q.ProfileDigest, Deadline: deadline}, Scope: scope, JobID: job.ID, WorkerID: q.WorkerID, LeaseDuration: q.LeaseDuration}, CommandID: commandID}, CommandID: commandID}, nil
}

func publicationQueueCommandID(kind, job, worker contracts.ID, version uint64) contracts.ID {
	material := string(kind) + "\x00" + string(job) + "\x00" + string(worker) + "\x00" + strconv.FormatUint(version, 10)
	sum := sha256.Sum256([]byte("memorys.publication-queue/1\n" + material))
	return contracts.ID("queue-" + hex.EncodeToString(sum[:]))
}
