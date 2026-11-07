package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrPublicationTaskControl = errors.New("publication task control unavailable")

type PublicationTaskControl struct{ DB *sql.DB }

var _ contracts.PublicationTaskControl = PublicationTaskControl{}

func (c PublicationTaskControl) CancelPublicationTask(ctx context.Context, input contracts.PublicationTaskControlRequest) (contracts.ResourceJob, error) {
	return c.control(ctx, input, "cancel")
}

func (c PublicationTaskControl) PausePublicationTask(ctx context.Context, input contracts.PublicationTaskControlRequest) (contracts.ResourceJob, error) {
	return c.control(ctx, input, "pause")
}

func (c PublicationTaskControl) control(ctx context.Context, input contracts.PublicationTaskControlRequest, action string) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || c.DB == nil || (action != "cancel" && action != "pause") || input.Scope.SpaceID == "" || len(input.Scope.KnowledgeBaseIDs) != 1 || input.Scope.SubjectID == "" || input.JobID == "" || input.ExpectedJobVersion == 0 || input.Request.RequestID == "" {
		return empty, ErrPublicationTaskControl
	}
	tx, err := c.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, ErrPublicationTaskControl
	}
	defer tx.Rollback()
	space, kb := input.Scope.SpaceID, input.Scope.KnowledgeBaseIDs[0]
	var raw []byte
	var state, version, attempt, fencing string
	var worker, attemptID, leaseUntil sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT payload,state,row_version::text,attempt::text,fencing_token::text,worker_id,attempt_id,lease_until FROM akos_withdraw_v1.publication_task_heads WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR UPDATE`, string(space), string(kb), string(input.JobID)).Scan(&raw, &state, &version, &attempt, &fencing, &worker, &attemptID, &leaseUntil); err != nil || len(raw) == 0 || len(raw) > 32768 {
		return empty, ErrPublicationTaskControl
	}
	var current contracts.Job
	if json.Unmarshal(raw, &current) != nil || current.ID != input.JobID || current.SpaceID != space || current.Stage != "publication.prepare" || current.State != state || current.RowVersion != input.ExpectedJobVersion || strconv.FormatUint(current.RowVersion, 10) != version || strconv.FormatUint(current.Attempt, 10) != attempt || strconv.FormatUint(current.FencingToken, 10) != fencing || (state != "queued" && state != "running") {
		return empty, ErrPublicationTaskControl
	}
	var documentID contracts.ID
	if err := tx.QueryRowContext(ctx, `SELECT document_id FROM akos_withdraw_v1.publication_intents WHERE space_id=$1::text AND kb_id=$2::text AND job_id=$3::text`, string(space), string(kb), string(input.JobID)).Scan(&documentID); err != nil || documentID == "" {
		return empty, ErrPublicationTaskControl
	}
	if current.RowVersion == ^uint64(0) || current.FencingToken == ^uint64(0) {
		return empty, ErrPublicationTaskControl
	}
	targetState := "failed"
	code, message := contracts.Code("cancelled"), "发布任务已取消。"
	retryable := false
	if action == "pause" {
		targetState = "paused"
		code, message, retryable = contracts.Code("paused"), "发布任务已暂停，可继续恢复。", true
	}
	current.State = targetState
	current.RowVersion++
	current.FencingToken++
	current.LeaseUntil = nil
	current.Error = &contracts.PublicError{Code: code, SafeMessage: message, RequestID: input.Request.RequestID, Retryable: retryable}
	jobBytes, err := json.Marshal(current)
	if err != nil {
		return empty, ErrPublicationTaskControl
	}
	updated, err := tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.publication_task_heads SET row_version=$4::numeric,state=$5::text,fencing_token=$6::numeric,worker_id=NULL,attempt_id=NULL,lease_until=NULL,payload=$7::jsonb WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text AND row_version=$8::numeric`, string(space), string(kb), string(input.JobID), strconv.FormatUint(current.RowVersion, 10), targetState, strconv.FormatUint(current.FencingToken, 10), string(jobBytes), strconv.FormatUint(input.ExpectedJobVersion, 10))
	if err != nil {
		return empty, ErrPublicationTaskControl
	}
	if n, err := updated.RowsAffected(); err != nil || n != 1 {
		return empty, ErrPublicationTaskControl
	}
	commandID, err := submissionID("publication-" + action + "-")
	if err != nil {
		return empty, ErrPublicationTaskControl
	}
	semantic := []byte(string(current.ProfileDigest) + "\x00" + string(input.JobID) + "\x00" + strconv.FormatUint(input.ExpectedJobVersion, 10))
	digest := sha256.Sum256(append([]byte("akos.publication-control/1\n"), semantic...))
	resultBytes, err := json.Marshal(map[string]any{"JobVersion": current.RowVersion, "Lease": map[string]any{"JobID": "", "WorkerID": "", "AttemptID": "", "FencingToken": 0}})
	if err != nil {
		return empty, ErrPublicationTaskControl
	}
	args := []any{string(space), string(kb), string(input.JobID), string(input.Scope.SubjectID), "claim", string(commandID), string(input.Request.RequestID), hex.EncodeToString(digest[:]), strconv.FormatUint(current.RowVersion, 10), string(resultBytes)}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_task_receipts(space_id,kb_id,job_id,worker_id,kind,command_id,request_id,request_digest,status,job_version,result) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::text,'superseded',$9::numeric,$10::jsonb)`, args...); err != nil {
		return empty, ErrPublicationTaskControl
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_task_audit(space_id,kb_id,job_id,job_version,worker_id,kind,command_id,request_id,payload) VALUES($1::text,$2::text,$3::text,$4::numeric,$5::text,$6::text,$7::text,$8::text,$9::jsonb)`, string(space), string(kb), string(input.JobID), strconv.FormatUint(current.RowVersion, 10), string(input.Scope.SubjectID), "claim", string(commandID), string(input.Request.RequestID), string(jobBytes)); err != nil {
		return empty, ErrPublicationTaskControl
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_task_outbox(space_id,kb_id,job_id,job_version,kind,payload) VALUES($1::text,$2::text,$3::text,$4::numeric,'claim',$5::jsonb)`, string(space), string(kb), string(input.JobID), strconv.FormatUint(current.RowVersion, 10), string(jobBytes)); err != nil {
		return empty, ErrPublicationTaskControl
	}
	if err = tx.Commit(); err != nil {
		return empty, ErrPublicationTaskControl
	}
	return contracts.ResourceJob{ResourceID: documentID, KnowledgeBaseID: kb, Job: current}, nil
}
