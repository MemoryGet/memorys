package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
)

var ErrParseTaskRepository = errors.New("parse task repository unavailable")

type ParseTaskRecord struct {
	Job     contracts.Job
	Payload json.RawMessage
	Command json.RawMessage
	Lease   contracts.Lease
}

type ParseTaskRepository struct{ DB *sql.DB }

var _ ingestion.ParseTaskCreator = ParseTaskRepository{}

// BoundParseTaskQueue adapts one durable task partition to ingestion.ParseWorker.
type BoundParseTaskQueue struct {
	Repository                             ParseTaskRepository
	Space, KnowledgeBase, TaskID, WorkerID contracts.ID
	LeaseDuration                          time.Duration
}

var _ ingestion.ParseTaskQueue = BoundParseTaskQueue{}

func (q BoundParseTaskQueue) ClaimTask(ctx context.Context) (ingestion.ParseTaskLease, error) {
	if q.LeaseDuration == 0 {
		q.LeaseDuration = 30 * time.Second
	}
	if q.TaskID == "" {
		id, err := q.Repository.NextQueued(ctx, q.Space, q.KnowledgeBase)
		if err != nil {
			return ingestion.ParseTaskLease{}, err
		}
		q.TaskID = id
	}
	lease, err := q.Repository.ClaimTask(ctx, q.Space, q.KnowledgeBase, q.TaskID, q.WorkerID, q.LeaseDuration)
	if err != nil {
		return ingestion.ParseTaskLease{}, err
	}
	lease.Space, lease.KnowledgeBase = q.Space, q.KnowledgeBase
	return lease, nil
}

func (q BoundParseTaskQueue) CompleteTask(ctx context.Context, lease ingestion.ParseTaskLease, result contracts.ID) error {
	space, kb := q.Space, q.KnowledgeBase
	if lease.Space != "" {
		space, kb = lease.Space, lease.KnowledgeBase
	}
	return q.Repository.CompleteTask(ctx, space, kb, lease.Lease, lease.JobVersion, result)
}

func (q BoundParseTaskQueue) FailTask(ctx context.Context, lease ingestion.ParseTaskLease, safe contracts.PublicError) error {
	space, kb := q.Space, q.KnowledgeBase
	if lease.Space != "" {
		space, kb = lease.Space, lease.KnowledgeBase
	}
	return q.Repository.FailTask(ctx, space, kb, lease.Lease, lease.JobVersion, safe)
}

// GlobalParseTaskQueue claims one queued or expired parse task across all
// partitions. The task row supplies its tenant binding; completion remains
// fenced by that binding and the returned lease.
type GlobalParseTaskQueue struct {
	Repository    ParseTaskRepository
	WorkerID      contracts.ID
	LeaseDuration time.Duration
}

var _ ingestion.ParseTaskQueue = GlobalParseTaskQueue{}

func (q GlobalParseTaskQueue) ClaimTask(ctx context.Context) (ingestion.ParseTaskLease, error) {
	if q.Repository.DB == nil || q.WorkerID == "" {
		return ingestion.ParseTaskLease{}, ErrParseTaskRepository
	}
	duration := q.LeaseDuration
	if duration == 0 {
		duration = 30 * time.Second
	}
	var space, kb, id string
	err := q.Repository.DB.QueryRowContext(ctx, `SELECT space_id,kb_id,id FROM akos_withdraw_v1.parse_task_heads WHERE state='queued' OR (state='running' AND lease_until<=clock_timestamp()) ORDER BY updated_at,id LIMIT 1`).Scan(&space, &kb, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return ingestion.ParseTaskLease{}, ingestion.ErrParseTaskNoWork
	}
	if err != nil || space == "" || kb == "" || id == "" {
		return ingestion.ParseTaskLease{}, ErrParseTaskRepository
	}
	lease, err := q.Repository.ClaimTask(ctx, contracts.ID(space), contracts.ID(kb), contracts.ID(id), q.WorkerID, duration)
	if err != nil {
		return ingestion.ParseTaskLease{}, err
	}
	lease.Space, lease.KnowledgeBase = contracts.ID(space), contracts.ID(kb)
	return lease, nil
}

func (q GlobalParseTaskQueue) CompleteTask(ctx context.Context, lease ingestion.ParseTaskLease, result contracts.ID) error {
	if lease.Space == "" || lease.KnowledgeBase == "" {
		return ErrParseTaskRepository
	}
	return q.Repository.CompleteTask(ctx, lease.Space, lease.KnowledgeBase, lease.Lease, lease.JobVersion, result)
}

func (q GlobalParseTaskQueue) FailTask(ctx context.Context, lease ingestion.ParseTaskLease, safe contracts.PublicError) error {
	if lease.Space == "" || lease.KnowledgeBase == "" {
		return ErrParseTaskRepository
	}
	return q.Repository.FailTask(ctx, lease.Space, lease.KnowledgeBase, lease.Lease, lease.JobVersion, safe)
}

func (p ParseTaskRepository) NextQueued(ctx context.Context, space, kb contracts.ID) (contracts.ID, error) {
	if ctx == nil || p.DB == nil || space == "" || kb == "" {
		return "", ErrParseTaskRepository
	}
	var id contracts.ID
	if err := p.DB.QueryRowContext(ctx, `SELECT id FROM akos_withdraw_v1.parse_task_heads WHERE space_id=$1 AND kb_id=$2 AND state='queued' ORDER BY id LIMIT 1`, string(space), string(kb)).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ingestion.ErrParseTaskNoWork
		}
		return "", ErrParseTaskRepository
	}
	return id, nil
}

func (p ParseTaskRepository) Create(ctx context.Context, space, kb contracts.ID, job contracts.Job, command json.RawMessage) error {
	if ctx == nil || p.DB == nil || space == "" || kb == "" || job.ID == "" || job.SpaceID != space || job.Stage != "document.parse" || job.State != "queued" || job.RowVersion == 0 || len(command) == 0 || len(command) > 1<<20 {
		return ErrParseTaskRepository
	}
	if json.Valid(command) == false {
		return ErrParseTaskRepository
	}
	payload, err := json.Marshal(struct {
		Job     contracts.Job   `json:"job"`
		Command json.RawMessage `json:"command"`
	}{job, command})
	if err != nil || len(payload) > 262144 {
		return ErrParseTaskRepository
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ErrParseTaskRepository
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.parse_task_heads(space_id,kb_id,id,row_version,state,attempt,fencing_token,payload) VALUES($1,$2,$3,$4::numeric,'queued',$5::numeric,$6::numeric,$7::jsonb)`, string(space), string(kb), string(job.ID), strconv.FormatUint(job.RowVersion, 10), strconv.FormatUint(job.Attempt, 10), strconv.FormatUint(job.FencingToken, 10), string(payload)); err != nil {
		return ErrParseTaskRepository
	}
	eventPayload, _ := json.Marshal(map[string]any{"task_id": job.ID, "state": "queued", "row_version": job.RowVersion})
	auditID := contracts.ID(string(job.ID) + "-1")
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.parse_task_audit(id,space_id,kb_id,task_id,actor_id,request_id,action,row_version,payload) VALUES($1,$2,$3,$4,$5,$6,'parse.task.created',$7::numeric,$8::jsonb)`, string(auditID), string(space), string(kb), string(job.ID), string(job.SpaceID), string(job.ID), strconv.FormatUint(job.RowVersion, 10), string(eventPayload)); err != nil {
		return ErrParseTaskRepository
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.parse_task_outbox(id,space_id,kb_id,task_id,event_type,row_version,payload) VALUES($1,$2,$3,$4,'parse.task.created',$5::numeric,$6::jsonb)`, string(auditID), string(space), string(kb), string(job.ID), strconv.FormatUint(job.RowVersion, 10), string(eventPayload)); err != nil {
		return ErrParseTaskRepository
	}
	if tx.Commit() != nil {
		return ErrParseTaskRepository
	}
	return nil
}

// CreateFromCommand is the typed upload/application boundary. The command is
// encoded as an opaque payload, while task identity/state remain database
// authority and can be claimed by a different process.
func (p ParseTaskRepository) CreateFromCommand(ctx context.Context, space, kb contracts.ID, job contracts.Job, command ingestion.DraftParseCommand) error {
	raw, err := json.Marshal(command)
	if err != nil || len(raw) > 1<<20 {
		return ErrParseTaskRepository
	}
	return p.Create(ctx, space, kb, job, raw)
}

func (p ParseTaskRepository) CreateParseTask(ctx context.Context, input ingestion.ParseTaskCreate) error {
	return p.CreateFromCommand(ctx, input.Space, input.KnowledgeBase, input.Job, input.Command)
}

func (p ParseTaskRepository) ClaimTask(ctx context.Context, space, kb, id, worker contracts.ID, duration time.Duration) (ingestion.ParseTaskLease, error) {
	record, err := p.Claim(ctx, space, kb, id, worker, duration)
	if err != nil {
		return ingestion.ParseTaskLease{}, err
	}
	return ingestion.ParseTaskLease{JobID: record.Job.ID, JobVersion: record.Job.RowVersion, Lease: record.Lease, Payload: append([]byte(nil), record.Command...)}, nil
}

func (p ParseTaskRepository) CompleteTask(ctx context.Context, space, kb contracts.ID, lease contracts.Lease, expectedVersion uint64, resultID contracts.ID) error {
	_, err := p.Complete(ctx, space, kb, lease, expectedVersion, resultID)
	return err
}

func (p ParseTaskRepository) FailTask(ctx context.Context, space, kb contracts.ID, lease contracts.Lease, expectedVersion uint64, safe contracts.PublicError) error {
	_, err := p.Fail(ctx, space, kb, lease, expectedVersion, safe)
	return err
}

func (p ParseTaskRepository) Read(ctx context.Context, space, kb, id contracts.ID) (ParseTaskRecord, error) {
	if ctx == nil || p.DB == nil || space == "" || kb == "" || id == "" {
		return ParseTaskRecord{}, ErrParseTaskRepository
	}
	return readParseTask(ctx, p.DB, space, kb, id, "")
}

// ReadAny resolves the durable tenant binding from the task row before the
// caller performs its own current authorization check. It never returns a
// public projection by itself.
func (p ParseTaskRepository) ReadAny(ctx context.Context, id contracts.ID) (ParseTaskRecord, contracts.ID, contracts.ID, error) {
	if ctx == nil || p.DB == nil || id == "" {
		return ParseTaskRecord{}, "", "", ErrParseTaskRepository
	}
	var space, kb string
	if err := p.DB.QueryRowContext(ctx, `SELECT space_id,kb_id FROM akos_withdraw_v1.parse_task_heads WHERE id=$1`, string(id)).Scan(&space, &kb); err != nil || space == "" || kb == "" {
		return ParseTaskRecord{}, "", "", ErrParseTaskRepository
	}
	record, err := p.Read(ctx, contracts.ID(space), contracts.ID(kb), id)
	if err != nil {
		return ParseTaskRecord{}, "", "", err
	}
	return record, contracts.ID(space), contracts.ID(kb), nil
}

func (p ParseTaskRepository) Claim(ctx context.Context, space, kb, id, worker contracts.ID, duration time.Duration) (ParseTaskRecord, error) {
	var empty ParseTaskRecord
	if ctx == nil || p.DB == nil || space == "" || kb == "" || id == "" || worker == "" || duration < time.Millisecond || duration > 30*time.Second {
		return empty, ErrParseTaskRepository
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, ErrParseTaskRepository
	}
	defer tx.Rollback()
	record, err := readParseTask(ctx, tx, space, kb, id, "FOR UPDATE")
	if err != nil {
		return empty, err
	}
	now := time.Now().UTC()
	if record.Job.State == "running" && record.Lease.ExpiresAt.After(now) || record.Job.State != "queued" && record.Job.State != "running" {
		return empty, ErrParseTaskRepository
	}
	if record.Job.RowVersion == ^uint64(0) || record.Job.Attempt == ^uint64(0) || record.Job.FencingToken == ^uint64(0) {
		return empty, ErrParseTaskRepository
	}
	attemptID, err := submissionID("parse-attempt-")
	if err != nil {
		return empty, ErrParseTaskRepository
	}
	job := record.Job
	job.State = "running"
	job.Attempt++
	job.FencingToken++
	job.RowVersion++
	lease := contracts.Lease{JobID: id, WorkerID: worker, AttemptID: attemptID, FencingToken: job.FencingToken, ExpiresAt: now.Add(duration)}
	job.LeaseUntil = &lease.ExpiresAt
	payload, err := marshalParseTaskPayload(job, record.Command)
	if err != nil || len(payload) > 262144 {
		return empty, ErrParseTaskRepository
	}
	if _, err = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.parse_task_heads SET row_version=$4::numeric,state='running',attempt=$5::numeric,fencing_token=$6::numeric,worker_id=$7,attempt_id=$8,lease_until=$9,payload=$10::jsonb,updated_at=clock_timestamp() WHERE space_id=$1 AND kb_id=$2 AND id=$3 AND row_version=$11::numeric`, string(space), string(kb), string(id), strconv.FormatUint(job.RowVersion, 10), strconv.FormatUint(job.Attempt, 10), strconv.FormatUint(job.FencingToken, 10), string(worker), string(attemptID), lease.ExpiresAt, string(payload), strconv.FormatUint(record.Job.RowVersion, 10)); err != nil {
		return empty, ErrParseTaskRepository
	}
	if err = tx.Commit(); err != nil {
		return empty, ErrParseTaskRepository
	}
	return ParseTaskRecord{Job: job, Payload: payload, Lease: lease}, nil
}

func (p ParseTaskRepository) Complete(ctx context.Context, space, kb contracts.ID, lease contracts.Lease, expectedVersion uint64, resultID contracts.ID) (ParseTaskRecord, error) {
	var empty ParseTaskRecord
	if ctx == nil || p.DB == nil || space == "" || kb == "" || lease.JobID == "" || lease.WorkerID == "" || lease.AttemptID == "" || lease.FencingToken == 0 || expectedVersion == 0 || resultID == "" {
		return empty, ErrParseTaskRepository
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, ErrParseTaskRepository
	}
	defer tx.Rollback()
	record, err := readParseTask(ctx, tx, space, kb, lease.JobID, "FOR UPDATE")
	if err != nil || record.Job.RowVersion != expectedVersion || record.Job.State != "running" || record.Lease != lease || !record.Lease.ExpiresAt.After(time.Now()) {
		return empty, ErrParseTaskRepository
	}
	if record.Job.RowVersion == ^uint64(0) {
		return empty, ErrParseTaskRepository
	}
	job := record.Job
	job.State, job.Progress = "completed", 1
	job.GenerationID = cloneContractID(resultID)
	job.LeaseUntil = nil
	job.RowVersion++
	payload, err := marshalParseTaskPayload(job, record.Command)
	if err != nil || len(payload) > 262144 {
		return empty, ErrParseTaskRepository
	}
	if _, err = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.parse_task_heads SET row_version=$4::numeric,state='completed',worker_id=NULL,attempt_id=NULL,lease_until=NULL,payload=$5::jsonb,error=NULL,updated_at=clock_timestamp() WHERE space_id=$1 AND kb_id=$2 AND id=$3 AND row_version=$6::numeric AND worker_id=$7 AND attempt_id=$8 AND fencing_token=$9::numeric`, string(space), string(kb), string(lease.JobID), strconv.FormatUint(job.RowVersion, 10), string(payload), strconv.FormatUint(expectedVersion, 10), string(lease.WorkerID), string(lease.AttemptID), strconv.FormatUint(lease.FencingToken, 10)); err != nil {
		return empty, ErrParseTaskRepository
	}
	if err = tx.Commit(); err != nil {
		return empty, ErrParseTaskRepository
	}
	return ParseTaskRecord{Job: job, Payload: payload}, nil
}

// Fail records a parser/preparation failure only while the exact running lease
// is still valid. A stale worker cannot overwrite a newer attempt, and an
// unknown commit effect is intentionally handled by the caller without using
// this method.
func (p ParseTaskRepository) Fail(ctx context.Context, space, kb contracts.ID, lease contracts.Lease, expectedVersion uint64, safe contracts.PublicError) (ParseTaskRecord, error) {
	var empty ParseTaskRecord
	if ctx == nil || p.DB == nil || space == "" || kb == "" || lease.JobID == "" || lease.WorkerID == "" || lease.AttemptID == "" || lease.FencingToken == 0 || expectedVersion == 0 || safe.Code == "" || safe.SafeMessage == "" || safe.RequestID == "" || len(safe.SafeMessage) > 10000 {
		return empty, ErrParseTaskRepository
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, ErrParseTaskRepository
	}
	defer tx.Rollback()
	record, err := readParseTask(ctx, tx, space, kb, lease.JobID, "FOR UPDATE")
	if err != nil || record.Job.RowVersion != expectedVersion || record.Job.State != "running" || record.Lease != lease || !record.Lease.ExpiresAt.After(time.Now()) {
		return empty, ErrParseTaskRepository
	}
	if record.Job.RowVersion == ^uint64(0) || record.Job.FencingToken == ^uint64(0) {
		return empty, ErrParseTaskRepository
	}
	job := record.Job
	errorCopy := safe
	job.State, job.Error = "failed", &errorCopy
	job.LeaseUntil = nil
	job.RowVersion++
	job.FencingToken++
	payload, err := marshalParseTaskPayload(job, record.Command)
	if err != nil || len(payload) > 262144 {
		return empty, ErrParseTaskRepository
	}
	errorBytes, err := json.Marshal(safe)
	if err != nil {
		return empty, ErrParseTaskRepository
	}
	if _, err = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.parse_task_heads SET row_version=$4::numeric,state='failed',fencing_token=$5::numeric,worker_id=NULL,attempt_id=NULL,lease_until=NULL,payload=$6::jsonb,error=$7::jsonb,updated_at=clock_timestamp() WHERE space_id=$1 AND kb_id=$2 AND id=$3 AND row_version=$8::numeric AND worker_id=$9 AND attempt_id=$10 AND fencing_token=$11::numeric`, string(space), string(kb), string(lease.JobID), strconv.FormatUint(job.RowVersion, 10), strconv.FormatUint(job.FencingToken, 10), string(payload), string(errorBytes), strconv.FormatUint(expectedVersion, 10), string(lease.WorkerID), string(lease.AttemptID), strconv.FormatUint(lease.FencingToken, 10)); err != nil {
		return empty, ErrParseTaskRepository
	}
	auditPayload, _ := json.Marshal(map[string]any{"task_id": lease.JobID, "state": "failed", "row_version": job.RowVersion, "error": safe.Code})
	auditID := contracts.ID(string(lease.JobID) + "-" + strconv.FormatUint(job.RowVersion, 10))
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.parse_task_audit(id,space_id,kb_id,task_id,actor_id,request_id,action,row_version,payload) VALUES($1,$2,$3,$4,$5,$6,'parse.task.failed',$7::numeric,$8::jsonb)`, string(auditID), string(space), string(kb), string(lease.JobID), string(lease.WorkerID), string(safe.RequestID), strconv.FormatUint(job.RowVersion, 10), string(auditPayload)); err != nil {
		return empty, ErrParseTaskRepository
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.parse_task_outbox(id,space_id,kb_id,task_id,event_type,row_version,payload) VALUES($1,$2,$3,$4,'parse.task.failed',$5::numeric,$6::jsonb)`, string(auditID), string(space), string(kb), string(lease.JobID), strconv.FormatUint(job.RowVersion, 10), string(auditPayload)); err != nil {
		return empty, ErrParseTaskRepository
	}
	if err = tx.Commit(); err != nil {
		return empty, ErrParseTaskRepository
	}
	return ParseTaskRecord{Job: job, Payload: payload}, nil
}

func (p ParseTaskRepository) Cancel(ctx context.Context, space, kb, id, actor, request contracts.ID, expected uint64) (ParseTaskRecord, error) {
	return p.control(ctx, space, kb, id, actor, request, expected, "cancel")
}

func (p ParseTaskRepository) Pause(ctx context.Context, space, kb, id, actor, request contracts.ID, expected uint64) (ParseTaskRecord, error) {
	return p.control(ctx, space, kb, id, actor, request, expected, "pause")
}

func (p ParseTaskRepository) Retry(ctx context.Context, space, kb, id, actor, request contracts.ID, expected uint64) (ParseTaskRecord, error) {
	return p.control(ctx, space, kb, id, actor, request, expected, "retry")
}

func (p ParseTaskRepository) control(ctx context.Context, space, kb, id, actor, request contracts.ID, expected uint64, action string) (ParseTaskRecord, error) {
	var empty ParseTaskRecord
	if ctx == nil || p.DB == nil || space == "" || kb == "" || id == "" || actor == "" || request == "" || expected == 0 || action != "cancel" && action != "pause" && action != "retry" {
		return empty, ErrParseTaskRepository
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return empty, ErrParseTaskRepository
	}
	defer tx.Rollback()
	record, err := readParseTask(ctx, tx, space, kb, id, "FOR UPDATE")
	if err != nil || record.Job.RowVersion != expected {
		return empty, ErrParseTaskRepository
	}
	if action == "cancel" || action == "pause" {
		if record.Job.State != "queued" && record.Job.State != "running" {
			return empty, ErrParseTaskRepository
		}
		if action == "pause" {
			record.Job.State = "paused"
			record.Job.Error = &contracts.PublicError{Code: "paused", SafeMessage: "解析任务已暂停，可继续恢复。", RequestID: request, Retryable: true}
		} else {
			record.Job.State = "cancelled"
			record.Job.Error = &contracts.PublicError{Code: "cancelled", SafeMessage: "解析任务已取消。", RequestID: request, Retryable: false}
		}
		record.Job.LeaseUntil = nil
	} else {
		if record.Job.State != "failed" && record.Job.State != "cancelled" && record.Job.State != "paused" {
			return empty, ErrParseTaskRepository
		}
		record.Job.State = "queued"
		record.Job.Attempt++
		record.Job.Error = nil
		record.Job.LeaseUntil = nil
	}
	if record.Job.RowVersion == ^uint64(0) {
		return empty, ErrParseTaskRepository
	}
	record.Job.RowVersion++
	record.Job.FencingToken++
	if record.Job.State == "queued" && record.Job.Attempt == 0 {
		record.Job.Attempt = 1
	}
	payload, err := json.Marshal(record.Job)
	if err != nil || len(payload) > 262144 {
		return empty, ErrParseTaskRepository
	}
	errorBytes := []byte("null")
	if record.Job.Error != nil {
		errorBytes, err = json.Marshal(record.Job.Error)
		if err != nil {
			return empty, ErrParseTaskRepository
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.parse_task_heads SET row_version=$4::numeric,state=$5,attempt=$6::numeric,fencing_token=$7::numeric,worker_id=NULL,attempt_id=NULL,lease_until=NULL,payload=$8::jsonb,error=$9::jsonb,updated_at=clock_timestamp() WHERE space_id=$1 AND kb_id=$2 AND id=$3 AND row_version=$10::numeric`, string(space), string(kb), string(id), strconv.FormatUint(record.Job.RowVersion, 10), record.Job.State, strconv.FormatUint(record.Job.Attempt, 10), strconv.FormatUint(record.Job.FencingToken, 10), string(payload), string(errorBytes), strconv.FormatUint(expected, 10)); err != nil {
		return empty, ErrParseTaskRepository
	}
	actionName := "parse.task.cancelled"
	if action == "retry" {
		actionName = "parse.task.retry_requested"
	} else if action == "pause" {
		actionName = "parse.task.paused"
	}
	auditPayload, _ := json.Marshal(map[string]any{"task_id": id, "state": record.Job.State, "row_version": record.Job.RowVersion})
	auditID := contracts.ID(string(id) + "-" + strconv.FormatUint(record.Job.RowVersion, 10))
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.parse_task_audit(id,space_id,kb_id,task_id,actor_id,request_id,action,row_version,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9::jsonb)`, string(auditID), string(space), string(kb), string(id), string(actor), string(request), actionName, strconv.FormatUint(record.Job.RowVersion, 10), string(auditPayload)); err != nil {
		return empty, ErrParseTaskRepository
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.parse_task_outbox(id,space_id,kb_id,task_id,event_type,row_version,payload) VALUES($1,$2,$3,$4,$5,$6::numeric,$7::jsonb)`, string(auditID), string(space), string(kb), string(id), actionName, strconv.FormatUint(record.Job.RowVersion, 10), string(auditPayload)); err != nil {
		return empty, ErrParseTaskRepository
	}
	if err = tx.Commit(); err != nil {
		return empty, ErrParseTaskRepository
	}
	return record, nil
}

type parseTaskQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readParseTask(ctx context.Context, q parseTaskQueryer, space, kb, id contracts.ID, lock string) (ParseTaskRecord, error) {
	if lock != "" && lock != "FOR UPDATE" {
		return ParseTaskRecord{}, ErrParseTaskRepository
	}
	query := `SELECT payload,row_version::text,state,attempt::text,fencing_token::text,worker_id,attempt_id,lease_until FROM akos_withdraw_v1.parse_task_heads WHERE space_id=$1 AND kb_id=$2 AND id=$3`
	if lock != "" {
		query += " " + lock
	}
	var raw []byte
	var version, state, attempt, fencing string
	var worker, attemptID sql.NullString
	var until sql.NullTime
	if q.QueryRowContext(ctx, query, string(space), string(kb), string(id)).Scan(&raw, &version, &state, &attempt, &fencing, &worker, &attemptID, &until) != nil || len(raw) == 0 || len(raw) > 262144 {
		return ParseTaskRecord{}, ErrParseTaskRepository
	}
	var job contracts.Job
	parsedVersion, versionErr := strconv.ParseUint(version, 10, 64)
	parsedAttempt, attemptErr := strconv.ParseUint(attempt, 10, 64)
	parsedFencing, fencingErr := strconv.ParseUint(fencing, 10, 64)
	if json.Unmarshal(raw, &job) != nil {
		return ParseTaskRecord{}, ErrParseTaskRepository
	}
	var envelope struct {
		Job     contracts.Job   `json:"job"`
		Command json.RawMessage `json:"command"`
	}
	if job.ID == "" && json.Unmarshal(raw, &envelope) == nil && envelope.Job.ID != "" {
		job = envelope.Job
	}
	if versionErr != nil || attemptErr != nil || fencingErr != nil || job.ID != id || job.SpaceID != space || job.Stage != "document.parse" || job.State != state || job.RowVersion != parsedVersion || job.Attempt != parsedAttempt || job.FencingToken != parsedFencing || job.RowVersion == 0 {
		return ParseTaskRecord{}, ErrParseTaskRepository
	}
	var lease contracts.Lease
	if state == "running" {
		if !worker.Valid || !attemptID.Valid || !until.Valid || job.LeaseUntil == nil || !job.LeaseUntil.Equal(until.Time) {
			return ParseTaskRecord{}, ErrParseTaskRepository
		}
		lease = contracts.Lease{JobID: id, WorkerID: contracts.ID(worker.String), AttemptID: contracts.ID(attemptID.String), FencingToken: parsedFencing, ExpiresAt: until.Time.UTC()}
	}
	command := json.RawMessage(nil)
	if json.Unmarshal(raw, &envelope) == nil && envelope.Job.ID != "" {
		command = append([]byte(nil), envelope.Command...)
	}
	return ParseTaskRecord{Job: job, Payload: append([]byte(nil), raw...), Command: command, Lease: lease}, nil
}

func cloneContractID(value contracts.ID) *contracts.ID {
	copy := value
	return &copy
}

func marshalParseTaskPayload(job contracts.Job, command json.RawMessage) ([]byte, error) {
	if len(command) == 0 {
		return json.Marshal(job)
	}
	return json.Marshal(struct {
		Job     contracts.Job   `json:"job"`
		Command json.RawMessage `json:"command"`
	}{job, command})
}

func parseTaskLeaseExpired(job contracts.Job, now time.Time) bool {
	return job.LeaseUntil == nil || !job.LeaseUntil.After(now)
}
