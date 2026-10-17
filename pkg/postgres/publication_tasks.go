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
)

// PublicationTasks supplies lease/owned-content ports only. Worker authority
// and the original publisher's current authority are separate required checks.
type PublicationTasks struct {
	DB                 *sql.DB
	Verify             VerifyScope
	AuthorizeTask      func(context.Context, document.PublicationTaskAccess) error
	AuthorizePublisher func(context.Context, document.PublicationStartPlan) error
}

var _ document.PublicationTaskStore = PublicationTasks{}

func (p PublicationTasks) Claim(ctx context.Context, r document.PublicationClaimRequest) (document.PublicationLeaseResult, error) {
	return p.mutate(ctx, document.PublicationTaskAccess{Kind: "claim", Request: r.Request, Scope: r.Scope, JobID: r.JobID, WorkerID: r.WorkerID, Duration: r.LeaseDuration, CommandID: r.CommandID})
}
func (p PublicationTasks) Renew(ctx context.Context, r document.PublicationRenewRequest) (document.PublicationLeaseResult, error) {
	return p.mutate(ctx, document.PublicationTaskAccess{Kind: "renew", Request: r.Request, Scope: r.Scope, JobID: r.Lease.JobID, WorkerID: r.Lease.WorkerID, Lease: r.Lease, Duration: r.Duration, CommandID: r.CommandID})
}

func validPublicationTaskAccess(c document.PublicationTaskAccess) bool {
	if c.Scope.Action != "publication.prepare" || c.Scope.SubjectID != c.WorkerID || c.Scope.PolicyVersion == 0 || len(c.Scope.KnowledgeBaseIDs) != 1 || !commandDigest(c.Request.ProfileDigest) || c.Request.Deadline.IsZero() {
		return false
	}
	for _, id := range []contracts.ID{c.Scope.ScopeID, c.Scope.SpaceID, c.Scope.KnowledgeBaseIDs[0], c.JobID, c.WorkerID, c.Request.RequestID} {
		if !commandID(string(id)) {
			return false
		}
	}
	if !publicationReadKind(c.Kind) && !commandID(string(c.CommandID)) {
		return false
	}
	if c.Kind != "claim" && (c.Lease.JobID != c.JobID || c.Lease.WorkerID != c.WorkerID || !commandID(string(c.Lease.AttemptID)) || c.Lease.FencingToken == 0 || c.Lease.ExpiresAt.IsZero()) {
		return false
	}
	return publicationReadKind(c.Kind) || c.Kind == "complete" && c.Duration == 0 || ((c.Kind == "claim" || c.Kind == "renew") && c.Duration >= time.Millisecond && c.Duration <= 30*time.Second && c.Duration%time.Millisecond == 0)
}

func publicationReadKind(kind string) bool {
	return kind == "read" || kind == "prepare_index" || kind == "inspect_index"
}

func copyPublicationTaskAccess(c document.PublicationTaskAccess) (document.PublicationTaskAccess, error) {
	c.Lease.ExpiresAt = c.Lease.ExpiresAt.UTC()
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > 65536 {
		return document.PublicationTaskAccess{}, document.ErrPublicationTaskRejected
	}
	var owned document.PublicationTaskAccess
	if json.Unmarshal(raw, &owned) != nil {
		return owned, document.ErrPublicationTaskRejected
	}
	return owned, nil
}

func (p PublicationTasks) within(ctx context.Context, c document.PublicationTaskAccess, fn func(context.Context, *session, document.PublicationStartPlan) error) error {
	return p.withinChecked(ctx, c, fn, nil)
}

func (p PublicationTasks) withinChecked(ctx context.Context, c document.PublicationTaskAccess, fn func(context.Context, *session, document.PublicationStartPlan) error, commitCheck func(context.Context, *sql.Tx) error) error {
	if ctx == nil || p.DB == nil || p.AuthorizeTask == nil || p.AuthorizePublisher == nil || !validPublicationTaskAccess(c) {
		return document.ErrPublicationTaskRejected
	}
	if _, ok := ctx.Deadline(); !ok {
		return document.ErrPublicationTaskRejected
	}
	ctx, cancel := context.WithDeadline(ctx, c.Request.Deadline)
	defer cancel()
	if publicationReadKind(c.Kind) {
		var leaseCancel context.CancelFunc
		ctx, leaseCancel = context.WithDeadline(ctx, c.Lease.ExpiresAt)
		defer leaseCancel()
	}
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > 65536 {
		return document.ErrPublicationTaskRejected
	}
	var owned document.PublicationTaskAccess
	if json.Unmarshal(raw, &owned) != nil {
		return document.ErrPublicationTaskRejected
	}
	c = owned
	var savedPlan []byte
	var readTx *sql.Tx
	var commitTx *sql.Tx
	verify := func(cx context.Context, scope contracts.AccessScope) error {
		if p.Verify == nil || p.Verify(cx, cloneScope(scope)) != nil {
			return ErrRepository
		}
		var access document.PublicationTaskAccess
		if json.Unmarshal(raw, &access) != nil || p.AuthorizeTask(cx, access) != nil {
			return ErrRepository
		}
		if savedPlan != nil {
			var plan document.PublicationStartPlan
			if json.Unmarshal(savedPlan, &plan) != nil || p.AuthorizePublisher(cx, plan) != nil {
				return ErrRepository
			}
		}
		if cx.Err() != nil {
			return ErrRepository
		}
		if readTx != nil {
			var now time.Time
			if readTx.QueryRowContext(cx, `SELECT clock_timestamp()`).Scan(&now) != nil || !c.Lease.ExpiresAt.After(now) {
				return ErrRepository
			}
		}
		if commitTx != nil && commitCheck != nil && commitCheck(cx, commitTx) != nil {
			return ErrRepository
		}
		return nil
	}
	return withinSession(ctx, p.DB, c.Scope, verify, func(cx context.Context, s *session) error {
		if err := s.enter(cx, c.Scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		var planBytes, initialBytes []byte
		if s.tx.QueryRowContext(cx, `SELECT p.payload,j.payload FROM akos_withdraw_v1.publication_intents p JOIN akos_withdraw_v1.publication_queued_jobs j ON j.space_id=p.space_id AND j.kb_id=p.kb_id AND j.operation_id=p.id AND j.id=p.job_id WHERE j.space_id=$1::text AND j.kb_id=$2::text AND j.id=$3::text`, string(c.Scope.SpaceID), string(c.Scope.KnowledgeBaseIDs[0]), string(c.JobID)).Scan(&planBytes, &initialBytes) != nil || len(planBytes) > 262144 {
			return ErrRepository
		}
		var plan document.PublicationStartPlan
		var initial contracts.Job
		if json.Unmarshal(planBytes, &plan) != nil || json.Unmarshal(initialBytes, &initial) != nil || plan.SpaceID != c.Scope.SpaceID || plan.KnowledgeBaseID != c.Scope.KnowledgeBaseIDs[0] || plan.Result.Job.ID != c.JobID || initial.ID != c.JobID || initial.OperationID != plan.Result.Operation.ID || initial.ProfileDigest != plan.Backend.ProfileDigest || initial.ProfileDigest != c.Request.ProfileDigest || initial.State != "queued" || initial.RowVersion != 1 {
			return ErrRepository
		}
		savedPlan = append([]byte(nil), planBytes...)
		var authPlan document.PublicationStartPlan
		if json.Unmarshal(planBytes, &authPlan) != nil || p.AuthorizePublisher(cx, authPlan) != nil || cx.Err() != nil {
			return ErrRepository
		}
		if publicationReadKind(c.Kind) {
			readTx = s.tx
		}
		if err := fn(cx, s, plan); err != nil {
			return err
		}
		commitTx = s.tx
		return nil
	})
}

type publicationTaskHead struct {
	job   contracts.Job
	lease contracts.Lease
}

func loadPublicationTaskHead(ctx context.Context, tx *sql.Tx, scope contracts.AccessScope, id contracts.ID, lock string) (publicationTaskHead, error) {
	var h publicationTaskHead
	var payload []byte
	var version, state, attempt, fence string
	var worker, attemptID, until sql.NullString
	if lock != "FOR UPDATE" && lock != "FOR SHARE" {
		return h, ErrRepository
	}
	if tx.QueryRowContext(ctx, `SELECT payload,row_version::text,state,attempt::text,fencing_token::text,worker_id,attempt_id,lease_until FROM akos_withdraw_v1.publication_task_heads WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text `+lock, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(id)).Scan(&payload, &version, &state, &attempt, &fence, &worker, &attemptID, &until) != nil || len(payload) > 32768 || json.Unmarshal(payload, &h.job) != nil || h.job.ID != id || h.job.SpaceID != scope.SpaceID || strconv.FormatUint(h.job.RowVersion, 10) != version || h.job.State != state || strconv.FormatUint(h.job.Attempt, 10) != attempt || strconv.FormatUint(h.job.FencingToken, 10) != fence {
		return h, ErrRepository
	}
	if state == "running" {
		if !worker.Valid || !attemptID.Valid || !until.Valid {
			return h, ErrRepository
		}
		expires, err := time.Parse(timeFormat, until.String)
		if err != nil || h.job.LeaseUntil == nil || !h.job.LeaseUntil.Equal(expires) {
			return h, ErrRepository
		}
		h.lease = contracts.Lease{JobID: id, WorkerID: contracts.ID(worker.String), AttemptID: contracts.ID(attemptID.String), FencingToken: h.job.FencingToken, ExpiresAt: expires}
	}
	return h, nil
}

func taskMutationError(err error) error {
	if errors.Is(err, ErrCommitUnknown) {
		return errors.Join(ErrCommitUnknown, document.ErrPublicationTaskUnknown)
	}
	if errors.Is(err, document.ErrPublicationSuperseded) {
		return document.ErrPublicationSuperseded
	}
	return document.ErrPublicationTaskRejected
}

func (p PublicationTasks) mutate(ctx context.Context, c document.PublicationTaskAccess) (document.PublicationLeaseResult, error) {
	var result document.PublicationLeaseResult
	var copyErr error
	c, copyErr = copyPublicationTaskAccess(c)
	if copyErr != nil {
		return result, copyErr
	}
	semantic, _ := json.Marshal(struct {
		Job, Worker contracts.ID
		Kind        string
		Profile     contracts.Digest
		Lease       contracts.Lease
		Duration    time.Duration
	}{c.JobID, c.WorkerID, c.Kind, c.Request.ProfileDigest, c.Lease, c.Duration})
	sum := sha256.Sum256(append([]byte("akos.pg-publication-task/1\n"), semantic...))
	digest := hex.EncodeToString(sum[:])
	superseded := false
	err := p.within(ctx, c, func(cx context.Context, s *session, plan document.PublicationStartPlan) error {
		args := []any{string(c.Scope.SpaceID), string(c.Scope.KnowledgeBaseIDs[0]), string(c.JobID)}
		var oldDigest, status string
		var receipt []byte
		err := s.tx.QueryRowContext(cx, `SELECT request_digest,status,result FROM akos_withdraw_v1.publication_task_receipts WHERE space_id=$1::text AND kb_id=$2::text AND worker_id=$3::text AND kind=$4::text AND command_id=$5::text`, args[0], args[1], string(c.WorkerID), c.Kind, string(c.CommandID)).Scan(&oldDigest, &status, &receipt)
		if err == nil {
			if oldDigest != digest || json.Unmarshal(receipt, &result) != nil || result.JobVersion == 0 || status != "granted" && status != "superseded" {
				return ErrRepository
			}
			if status == "granted" && (result.Lease.JobID != c.JobID || result.Lease.WorkerID != c.WorkerID || result.Lease.FencingToken == 0 || !commandID(string(result.Lease.AttemptID)) || result.Lease.ExpiresAt.IsZero()) {
				return ErrRepository
			}
			superseded = status == "superseded"
			return nil
		}
		if err != sql.ErrNoRows {
			return ErrRepository
		}
		authorityErr := lockPublicationTaskAuthority(cx, s.tx, plan)
		if authorityErr != nil && !errors.Is(authorityErr, document.ErrPublicationSuperseded) {
			return authorityErr
		}
		head, err := loadPublicationTaskHead(cx, s.tx, c.Scope, c.JobID, "FOR UPDATE")
		if err != nil {
			return err
		}
		var now time.Time
		if s.tx.QueryRowContext(cx, `SELECT clock_timestamp()`).Scan(&now) != nil {
			return ErrRepository
		}
		deadline, _ := cx.Deadline()
		var attemptID contracts.ID
		if c.Kind == "claim" {
			attemptID, err = submissionID("attempt-")
			if err != nil {
				return err
			}
		}
		job, lease, err := document.TransitionPublicationLease(head.job, head.lease, c.Lease, c.WorkerID, attemptID, c.Kind, c.Duration, now.UTC(), deadline)
		if err != nil {
			return err
		}
		if errors.Is(authorityErr, document.ErrPublicationSuperseded) {
			job, err = document.SupersedePublicationJob(head.job, c.Request.RequestID)
			lease = contracts.Lease{}
			superseded = true
		}
		if err != nil {
			return err
		}
		jobBytes, err := json.Marshal(job)
		if err != nil {
			return ErrRepository
		}
		var worker, attemptValue, until any
		if job.State == "running" {
			worker = string(lease.WorkerID)
			attemptValue = string(lease.AttemptID)
			until = lease.ExpiresAt.Format(timeFormat)
		}
		updated, err := s.tx.ExecContext(cx, `UPDATE akos_withdraw_v1.publication_task_heads SET row_version=$5::numeric,state=$6::text,attempt=$7::numeric,fencing_token=$8::numeric,worker_id=$9::text,attempt_id=$10::text,lease_until=$11::text,payload=$12::jsonb WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text AND row_version=$4::numeric`, append(args, strconv.FormatUint(head.job.RowVersion, 10), strconv.FormatUint(job.RowVersion, 10), job.State, strconv.FormatUint(job.Attempt, 10), strconv.FormatUint(job.FencingToken, 10), worker, attemptValue, until, string(jobBytes))...)
		if err != nil {
			return ErrRepository
		}
		if n, err := updated.RowsAffected(); err != nil || n != 1 {
			return ErrRepository
		}
		result = document.PublicationLeaseResult{Lease: lease, JobVersion: job.RowVersion}
		resultBytes, _ := json.Marshal(result)
		status = "granted"
		if superseded {
			status = "superseded"
		}
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_task_receipts(space_id,kb_id,job_id,worker_id,kind,command_id,request_id,request_digest,status,job_version,result) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::text,$9::text,$10::numeric,$11::jsonb)`, append(args, string(c.WorkerID), c.Kind, string(c.CommandID), string(c.Request.RequestID), digest, status, strconv.FormatUint(job.RowVersion, 10), string(resultBytes))...); err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_task_audit(space_id,kb_id,job_id,job_version,worker_id,kind,command_id,request_id,payload) VALUES($1::text,$2::text,$3::text,$4::numeric,$5::text,$6::text,$7::text,$8::text,$9::jsonb)`, append(args, strconv.FormatUint(job.RowVersion, 10), string(c.WorkerID), c.Kind, string(c.CommandID), string(c.Request.RequestID), string(jobBytes))...); err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_task_outbox(space_id,kb_id,job_id,job_version,kind,payload) VALUES($1::text,$2::text,$3::text,$4::numeric,$5::text,$6::jsonb)`, append(args, strconv.FormatUint(job.RowVersion, 10), c.Kind, string(jobBytes))...); err != nil {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return document.PublicationLeaseResult{}, taskMutationError(err)
	}
	if superseded {
		return document.PublicationLeaseResult{}, document.ErrPublicationSuperseded
	}
	return result, nil
}

func (p PublicationTasks) ReadOwned(ctx context.Context, r document.PublicationReadWorkRequest) (document.PublicationWork, error) {
	var work document.PublicationWork
	c := document.PublicationTaskAccess{Kind: "read", Request: r.Request, Scope: r.Scope, JobID: r.Lease.JobID, WorkerID: r.Lease.WorkerID, Lease: r.Lease}
	var copyErr error
	c, copyErr = copyPublicationTaskAccess(c)
	if copyErr != nil {
		return work, copyErr
	}
	err := p.within(ctx, c, func(cx context.Context, s *session, plan document.PublicationStartPlan) error {
		if err := lockPublicationTaskAuthority(cx, s.tx, plan); err != nil {
			return err
		}
		head, err := loadPublicationTaskHead(cx, s.tx, c.Scope, c.JobID, "FOR SHARE")
		if err != nil {
			return err
		}
		var now time.Time
		if s.tx.QueryRowContext(cx, `SELECT clock_timestamp()`).Scan(&now) != nil || head.job.State != "running" || !document.SamePublicationLease(head.lease, c.Lease) || !head.lease.ExpiresAt.After(now) {
			return document.ErrPublicationTaskRejected
		}
		work, err = readPublicationWork(cx, s.tx, plan, head)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, document.ErrPublicationSuperseded) {
			return document.PublicationWork{}, document.ErrPublicationSuperseded
		}
		return document.PublicationWork{}, document.ErrPublicationTaskRejected
	}
	return work, nil
}

func readPublicationWork(cx context.Context, tx *sql.Tx, plan document.PublicationStartPlan, head publicationTaskHead) (document.PublicationWork, error) {
	if plan.Review.Revision.DocumentID != plan.Result.Operation.DocumentID || plan.Review.Revision.ID != plan.Result.Operation.RevisionID {
		return document.PublicationWork{}, ErrRepository
	}
	revision, payload, err := readFrozenRevision(cx, tx, plan.SpaceID, plan.KnowledgeBaseID, plan.Review.Revision)
	if err != nil {
		return document.PublicationWork{}, err
	}
	work := document.PublicationWork{Plan: plan, Job: head.job, Lease: head.lease, Revision: revision, FrozenPayload: append([]byte(nil), payload...)}
	return work, nil
}
