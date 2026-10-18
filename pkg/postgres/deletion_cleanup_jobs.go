package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// Default PG coordinator verifies native index tombstones/rows before completing.
// Other backend effects require a different verified implementation of the port.
type DeletionCleanupJobs struct {
	DB               *sql.DB
	Verify           VerifyScope
	AuthorizeJob     func(context.Context, contracts.AccessScope, contracts.ResourceJob) error
	AuthorizeControl func(context.Context, contracts.AccessScope, contracts.ResourceJob) error
}

func (p DeletionCleanupJobs) within(ctx context.Context, input contracts.DeletionCleanupRequest, fn func(context.Context, *session, deletionJobView) error) error {
	return p.withinAction(ctx, input, "publication.cleanup.execute", p.AuthorizeJob, fn)
}

func (p DeletionCleanupJobs) withinAction(ctx context.Context, input contracts.DeletionCleanupRequest, action contracts.Action, authorize func(context.Context, contracts.AccessScope, contracts.ResourceJob) error, fn func(context.Context, *session, deletionJobView) error) error {
	q, e := copyQuery(input)
	if ctx == nil || e != nil || authorize == nil || q.Request.Deadline.IsZero() || !commandID(string(q.Request.RequestID)) || !commandID(string(q.JobID)) || q.Scope.Action != action || q.Scope.SubjectID != q.WorkerID {
		return ErrRepository
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	return withinSession(ctx, p.DB, q.Scope, p.Verify, func(c context.Context, s *session) error {
		view, e := loadDeletionJobView(c, s.tx, q.JobID, true)
		if e != nil {
			return e
		}
		if view.record.Job.ProfileDigest != q.Request.ProfileDigest || view.record.Job.SpaceID != q.Scope.SpaceID || len(q.Scope.KnowledgeBaseIDs) != 1 || view.kb != q.Scope.KnowledgeBaseIDs[0] {
			return ErrRepository
		}
		scope := cloneScope(q.Scope)
		record, e := copyQuery(view.record)
		if e != nil {
			return e
		}
		if e = authorize(c, scope, record); e != nil {
			return e
		}
		if e = fn(c, s, view); e != nil {
			return e
		}
		fresh, loadErr := loadDeletionJobView(c, s.tx, q.JobID, true)
		if loadErr != nil {
			return loadErr
		}
		record, e = copyQuery(fresh.record)
		if e != nil {
			return e
		}
		return authorize(c, cloneScope(q.Scope), record)
	})
}

func (p DeletionCleanupJobs) CancelDeletionCleanup(ctx context.Context, input contracts.DeletionCleanupControlRequest) (contracts.ResourceJob, error) {
	var out contracts.ResourceJob
	if input.Scope.SubjectID == "" || input.ExpectedJobVersion == 0 {
		return out, ErrRepository
	}
	q := contracts.DeletionCleanupRequest{Request: input.Request, Scope: input.Scope, JobID: input.JobID, WorkerID: input.Scope.SubjectID}
	e := p.withinAction(ctx, q, "publication.cleanup.control", p.AuthorizeControl, func(c context.Context, s *session, v deletionJobView) error {
		if v.record.Job.RowVersion != input.ExpectedJobVersion || v.record.Job.State != "queued" && v.record.Job.State != "running" {
			return ErrRepository
		}
		var active bool
		if e := s.tx.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=$1 AND x.state='running' AND x.lease_until>clock_timestamp())`, input.JobID).Scan(&active); e != nil || active {
			return ErrRepository
		}
		if _, e := s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.document_delete_job_control_audit(id,request_id,job_id,space_id,kb_id,actor_id,expected_row_version,result_row_version,action) VALUES($1,$1,$2,$3,$4,$5,$6::numeric,$7::numeric,'document.delete.cleanup.cancelled')`, input.Request.RequestID, input.JobID, v.record.Job.SpaceID, v.kb, input.Scope.SubjectID, strconv.FormatUint(v.record.Job.RowVersion, 10), strconv.FormatUint(v.record.Job.RowVersion+1, 10)); e != nil {
			return ErrRepository
		}
		payload, e := json.Marshal(map[string]any{"job_id": input.JobID, "request_id": input.Request.RequestID, "actor_id": input.Scope.SubjectID, "state": "cancelled", "row_version": v.record.Job.RowVersion + 1})
		if e != nil {
			return ErrRepository
		}
		if _, e = s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.document_delete_job_control_outbox(id,audit_id,job_id,space_id,kb_id,event_type,aggregate_version,payload) VALUES($1,$1,$2,$3,$4,$5,$6::numeric,$7::jsonb)`, input.Request.RequestID, input.JobID, v.record.Job.SpaceID, v.kb, contracts.DeletionCleanupCancelledEvent, strconv.FormatUint(v.record.Job.RowVersion+1, 10), string(payload)); e != nil {
			return ErrRepository
		}
		if _, e := s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.document_delete_job_progress SET state='cancelled',row_version=row_version+1,error_request_id=$2 WHERE job_id=$1`, input.JobID, input.Request.RequestID); e != nil {
			return ErrRepository
		}
		fresh, e := loadDeletionJobView(c, s.tx, input.JobID, true)
		if e != nil {
			return e
		}
		out = fresh.record
		return nil
	})
	if e != nil {
		return contracts.ResourceJob{}, e
	}
	return out, nil
}

// RetryDeletionCleanup is deliberately narrower than a generic task retry.
// Only a failed aggregate whose non-success targets are all known failures can
// be reset. Unknown effects remain terminal until an operator reconciles the
// external backend; an active target lease also blocks the control decision.
func (p DeletionCleanupJobs) RetryDeletionCleanup(ctx context.Context, input contracts.DeletionCleanupControlRequest) (contracts.ResourceJob, error) {
	var out contracts.ResourceJob
	if input.Scope.SubjectID == "" || input.ExpectedJobVersion == 0 {
		return out, ErrRepository
	}
	q := contracts.DeletionCleanupRequest{Request: input.Request, Scope: input.Scope, JobID: input.JobID, WorkerID: input.Scope.SubjectID}
	e := p.withinAction(ctx, q, "publication.cleanup.control", p.AuthorizeControl, func(c context.Context, s *session, v deletionJobView) error {
		if v.record.Job.RowVersion != input.ExpectedJobVersion || v.record.Job.State != "failed" {
			return ErrRepository
		}
		var active, unknown, pending int
		if e := s.tx.QueryRowContext(c, `SELECT
			(SELECT count(*) FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=$1 AND x.state='running' AND x.lease_until>clock_timestamp()),
			(SELECT count(*) FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=$1 AND x.state='unknown'),
			(SELECT count(*) FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=$1 AND x.state IN('pending','running'))`, input.JobID).Scan(&active, &unknown, &pending); e != nil || active != 0 || unknown != 0 || pending != 0 {
			return ErrRepository
		}
		if _, e := s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.document_delete_job_control_audit(id,request_id,job_id,space_id,kb_id,actor_id,expected_row_version,result_row_version,action) VALUES($1,$1,$2,$3,$4,$5,$6::numeric,$7::numeric,'document.delete.cleanup.retry_requested')`, input.Request.RequestID, input.JobID, v.record.Job.SpaceID, v.kb, input.Scope.SubjectID, strconv.FormatUint(v.record.Job.RowVersion, 10), strconv.FormatUint(v.record.Job.RowVersion+1, 10)); e != nil {
			return ErrRepository
		}
		payload, e := json.Marshal(map[string]any{"job_id": input.JobID, "request_id": input.Request.RequestID, "actor_id": input.Scope.SubjectID, "state": "running", "row_version": v.record.Job.RowVersion + 1})
		if e != nil {
			return ErrRepository
		}
		if _, e = s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.document_delete_job_control_outbox(id,audit_id,job_id,space_id,kb_id,event_type,aggregate_version,payload) VALUES($1,$1,$2,$3,$4,$5,$6::numeric,$7::jsonb)`, input.Request.RequestID, input.JobID, v.record.Job.SpaceID, v.kb, contracts.DeletionCleanupRetryEvent, strconv.FormatUint(v.record.Job.RowVersion+1, 10), string(payload)); e != nil {
			return ErrRepository
		}
		if _, e = s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.cleanup_progress x SET state='pending',worker_id=NULL,lease_until=NULL,result_reference=NULL,row_version=row_version+1 WHERE EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets m WHERE m.job_id=$1 AND m.plan_id=x.plan_id AND m.target_id=x.target_id) AND x.state='failed'`, input.JobID); e != nil {
			return ErrRepository
		}
		if _, e = s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.document_delete_job_progress SET state='running',attempt=attempt+1,row_version=row_version+1,error_request_id=NULL,completed_targets=(SELECT count(*) FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=m.plan_id AND x.target_id=m.target_id WHERE m.job_id=$1 AND x.state='succeeded') WHERE job_id=$1 AND state='failed' AND row_version=$2::numeric`, input.JobID, strconv.FormatUint(input.ExpectedJobVersion, 10)); e != nil {
			return ErrRepository
		}
		fresh, e := loadDeletionJobView(c, s.tx, input.JobID, true)
		if e != nil {
			return e
		}
		out = fresh.record
		return nil
	})
	if e != nil {
		return contracts.ResourceJob{}, e
	}
	return out, nil
}

func (p DeletionCleanupJobs) NextDeletionCleanup(ctx context.Context, q contracts.DeletionCleanupRequest) (contracts.DeletionCleanupSelection, error) {
	var out contracts.DeletionCleanupSelection
	e := p.within(ctx, q, func(c context.Context, s *session, v deletionJobView) error {
		if v.record.Job.State == "queued" {
			_, e := s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.document_delete_job_progress p SET state='running',attempt=attempt+1,row_version=row_version+1,completed_targets=(SELECT count(*) FROM akos_withdraw_v1.document_delete_job_targets t JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=t.plan_id AND x.target_id=t.target_id WHERE t.job_id=p.job_id AND x.state='succeeded') WHERE job_id=$1`, q.JobID)
			if e != nil {
				return ErrRepository
			}
			v, e = loadDeletionJobView(c, s.tx, q.JobID, true)
			if e != nil {
				return e
			}
		}
		out.Record = v.record
		if v.record.Job.State != "running" {
			return nil
		}
		e := s.tx.QueryRowContext(c, `SELECT t.plan_id,t.target_id FROM akos_withdraw_v1.document_delete_job_targets t JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=t.plan_id AND x.target_id=t.target_id WHERE t.job_id=$1 AND (x.state='pending' OR (x.state='running' AND x.lease_until<=clock_timestamp())) ORDER BY t.plan_id,t.target_id LIMIT 1`, q.JobID).Scan(&out.PlanID, &out.TargetID)
		if e == sql.ErrNoRows {
			return nil
		}
		if e != nil {
			return ErrRepository
		}
		out.HasTarget = true
		return nil
	})
	if e != nil {
		return contracts.DeletionCleanupSelection{}, e
	}
	return out, nil
}

func (p DeletionCleanupJobs) ReconcileDeletionCleanup(ctx context.Context, q contracts.DeletionCleanupRequest) (contracts.ResourceJob, error) {
	var out contracts.ResourceJob
	e := p.within(ctx, q, func(c context.Context, s *session, v deletionJobView) error {
		out = v.record
		if v.record.Job.State != "running" {
			return nil
		}
		var total, done, bad int64
		e := s.tx.QueryRowContext(c, `SELECT count(*),count(*) FILTER(WHERE x.state='succeeded'),count(*) FILTER(WHERE x.state IN('failed','unknown')) FROM akos_withdraw_v1.document_delete_job_targets t JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=t.plan_id AND x.target_id=t.target_id WHERE t.job_id=$1`, q.JobID).Scan(&total, &done, &bad)
		if e != nil || total < 0 || uint64(total) != v.total {
			return ErrRepository
		}
		state := "running"
		var errorID any
		if bad > 0 {
			state = "failed"
			errorID = q.Request.RequestID
		} else if done == total {
			// Independent actual default-backend proof, not successful worker metadata.
			var invalid bool
			e = s.tx.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.document_delete_job_targets m JOIN akos_withdraw_v1.cleanup_targets t ON t.plan_id=m.plan_id AND t.id=m.target_id WHERE m.job_id=$1 AND (t.kind<>'index' OR NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=t.generation_id) OR EXISTS(SELECT 1 FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=t.generation_id)))`, q.JobID).Scan(&invalid)
			if e != nil || invalid {
				return ErrRepository
			}
			state = "completed"
		}
		if state != v.record.Job.State || uint64(done) != v.completed {
			if _, e = s.tx.ExecContext(c, `UPDATE akos_withdraw_v1.document_delete_job_progress SET state=$2,completed_targets=$3::numeric,row_version=row_version+1,error_request_id=$4 WHERE job_id=$1`, q.JobID, state, strconv.FormatInt(done, 10), errorID); e != nil {
				return ErrRepository
			}
		}
		fresh, e := loadDeletionJobView(c, s.tx, q.JobID, true)
		if e != nil {
			return e
		}
		out = fresh.record
		return nil
	})
	if e != nil {
		return contracts.ResourceJob{}, e
	}
	return out, nil
}

var _ contracts.DeletionCleanupStore = DeletionCleanupJobs{}
