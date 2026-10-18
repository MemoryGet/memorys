package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type deletionJobView struct {
	record           contracts.ResourceJob
	kb               contracts.ID
	total, completed uint64
}
type jobQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadDeletionJobView(ctx context.Context, db jobQueryer, id contracts.ID, lock bool) (deletionJobView, error) {
	var out deletionJobView
	var raw []byte
	var space, operation contracts.ID
	var profile contracts.Digest
	var state, attempt, version, completed, total string
	var errorRequest sql.NullString
	query := `SELECT j.document_id,j.kb_id,j.space_id,j.operation_id,j.profile_digest,j.job,p.state,p.attempt::text,p.row_version::text,p.completed_targets::text,p.total_targets::text,p.error_request_id FROM akos_withdraw_v1.document_delete_jobs j
 JOIN akos_withdraw_v1.document_delete_receipts r ON r.job_id=j.id AND r.operation_id=j.operation_id AND r.history_id=j.history_id AND r.document_id=j.document_id AND r.space_id=j.space_id AND r.kb_id=j.kb_id
 JOIN akos_withdraw_v1.command_operations o ON o.id=j.operation_id AND o.history_id=j.history_id AND o.action='delete' AND o.state='committed'
 JOIN akos_withdraw_v1.documents d ON d.id=j.document_id AND d.space_id=j.space_id AND d.kb_id=j.kb_id AND d.deleted
 JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id
 WHERE j.id=$1`
	if lock {
		query += " FOR UPDATE OF p"
	}
	e := db.QueryRowContext(ctx, query, id).Scan(&out.record.ResourceID, &out.kb, &space, &operation, &profile, &raw, &state, &attempt, &version, &completed, &total, &errorRequest)
	if e == sql.ErrNoRows {
		return out, contracts.ErrJobReadDenied
	}
	if e != nil || len(raw) > 65536 {
		return out, ErrRepository
	}
	out.record.KnowledgeBaseID = out.kb
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out.record.Job) != nil {
		return out, ErrRepository
	}
	job := &out.record.Job
	if job.ID != id || job.SpaceID != space || job.OperationID != operation || job.ProfileDigest != profile || !commandID(string(out.record.ResourceID)) || job.Stage != "document.delete.cleanup" || job.State != "queued" || job.RowVersion != 1 || job.Attempt != 0 || job.FencingToken != 0 || job.LeaseUntil != nil || job.Progress != 0 || job.Error != nil {
		return out, ErrRepository
	}
	job.Attempt, e = strconv.ParseUint(attempt, 10, 64)
	if e != nil {
		return out, ErrRepository
	}
	job.RowVersion, e = strconv.ParseUint(version, 10, 64)
	if e != nil || job.RowVersion == 0 {
		return out, ErrRepository
	}
	out.total, e = strconv.ParseUint(total, 10, 64)
	if e != nil {
		return out, ErrRepository
	}
	out.completed, e = strconv.ParseUint(completed, 10, 64)
	if e != nil || out.completed > out.total {
		return out, ErrRepository
	}
	job.State = state
	switch state {
	case "queued", "running", "completed", "failed", "cancelled":
	default:
		return out, ErrRepository
	}
	if out.total > 0 {
		job.Progress = float64(out.completed) / float64(out.total)
	} else if state == "completed" {
		job.Progress = 1
	}
	if state == "failed" {
		if !errorRequest.Valid {
			return out, ErrRepository
		}
		job.Error = &contracts.PublicError{Code: "dependency_unavailable", SafeMessage: "Cleanup requires reconciliation", RequestID: contracts.ID(errorRequest.String)}
	}
	if state == "cancelled" {
		if !errorRequest.Valid {
			return out, ErrRepository
		}
		job.Error = &contracts.PublicError{Code: "conflict", SafeMessage: "Cleanup canceled", RequestID: contracts.ID(errorRequest.String)}
	}
	return out, nil
}
