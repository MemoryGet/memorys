package localcli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

// webJob is the small, local-workbench projection of a publication task. The
// durable job payload remains the source of truth; this projection deliberately
// omits worker and fencing credentials.
type webJob struct {
	ID            contracts.ID           `json:"job_id"`
	OperationID   contracts.ID           `json:"operation_id"`
	RevisionID    contracts.ID           `json:"revision_id,omitempty"`
	HistoryID     contracts.ID           `json:"history_id,omitempty"`
	ProfileDigest contracts.Digest       `json:"-"`
	State         string                 `json:"state"`
	Stage         string                 `json:"stage"`
	Attempt       uint64                 `json:"attempt"`
	Progress      float64                `json:"progress"`
	RowVersion    uint64                 `json:"row_version"`
	LeaseUntil    *time.Time             `json:"lease_until,omitempty"`
	Error         *contracts.PublicError `json:"error,omitempty"`
	CreatedAt     time.Time              `json:"created_at,omitempty"`
}

type webTask struct {
	Kind        string           `json:"kind"`
	DocumentID  contracts.ID     `json:"document_id"`
	DraftID     contracts.ID     `json:"draft_id,omitempty"`
	RevisionID  contracts.ID     `json:"revision_id,omitempty"`
	ContentHash contracts.Digest `json:"content_hash,omitempty"`
	Job         *webJob          `json:"job"`
}

type taskListFilter struct {
	Kind          string
	State         string
	DocumentID    contracts.ID
	ErrorCode     contracts.Code
	CreatedAfter  time.Time
	CreatedBefore time.Time
}

func parseTaskListFilter(r *http.Request) (taskListFilter, error) {
	if r == nil {
		return taskListFilter{}, errors.New("任务筛选不可用")
	}
	q := r.URL.Query()
	filter := taskListFilter{Kind: q.Get("kind"), State: q.Get("state"), DocumentID: contracts.ID(q.Get("document_id")), ErrorCode: contracts.Code(q.Get("error_code"))}
	for _, key := range []string{"kind", "state", "document_id", "error_code", "created_after", "created_before"} {
		if len(q[key]) > 1 {
			return taskListFilter{}, errors.New("任务筛选参数重复")
		}
	}
	if filter.Kind != "" && filter.Kind != "parse" && filter.Kind != "publication" && filter.Kind != "deletion" {
		return taskListFilter{}, errors.New("任务类型无效")
	}
	if filter.State != "" && filter.State != "queued" && filter.State != "running" && filter.State != "paused" && filter.State != "completed" && filter.State != "failed" && filter.State != "cancelled" {
		return taskListFilter{}, errors.New("任务状态无效")
	}
	if filter.DocumentID != "" && (len(filter.DocumentID) > 200 || strings.ContainsAny(string(filter.DocumentID), "\r\n")) {
		return taskListFilter{}, errors.New("文档标识无效")
	}
	if filter.ErrorCode != "" && (len(filter.ErrorCode) > 100 || strings.ContainsAny(string(filter.ErrorCode), "\r\n")) {
		return taskListFilter{}, errors.New("失败原因无效")
	}
	for key, target := range map[string]*time.Time{"created_after": &filter.CreatedAfter, "created_before": &filter.CreatedBefore} {
		if value := q.Get(key); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return taskListFilter{}, errors.New("任务时间筛选无效")
			}
			*target = parsed
		}
	}
	if !filter.CreatedAfter.IsZero() && !filter.CreatedBefore.IsZero() && !filter.CreatedAfter.Before(filter.CreatedBefore) {
		return taskListFilter{}, errors.New("任务时间范围无效")
	}
	for key, values := range q {
		if key != "kind" && key != "state" && key != "document_id" && key != "error_code" && key != "created_after" && key != "created_before" {
			if len(values) > 0 {
				return taskListFilter{}, errors.New("未知任务筛选参数")
			}
		}
	}
	return filter, nil
}

func filterTasks(tasks []webTask, filter taskListFilter) []webTask {
	if filter.Kind == "" && filter.State == "" && filter.DocumentID == "" && filter.ErrorCode == "" && filter.CreatedAfter.IsZero() && filter.CreatedBefore.IsZero() {
		return tasks
	}
	out := make([]webTask, 0, len(tasks))
	for _, task := range tasks {
		if filter.Kind != "" && task.Kind != filter.Kind || filter.DocumentID != "" && task.DocumentID != filter.DocumentID || filter.State != "" && (task.Job == nil || task.Job.State != filter.State) || filter.ErrorCode != "" && (task.Job == nil || task.Job.Error == nil || task.Job.Error.Code != filter.ErrorCode) || !filter.CreatedAfter.IsZero() && (task.Job == nil || task.Job.CreatedAt.IsZero() || task.Job.CreatedAt.Before(filter.CreatedAfter)) || !filter.CreatedBefore.IsZero() && (task.Job == nil || task.Job.CreatedAt.IsZero() || !task.Job.CreatedAt.Before(filter.CreatedBefore)) {
			continue
		}
		out = append(out, task)
	}
	return out
}

func projectWebJob(record contracts.ResourceJob) *webJob {
	return &webJob{ID: record.Job.ID, OperationID: record.Job.OperationID, RevisionID: idValue(record.Job.RevisionID), ProfileDigest: record.Job.ProfileDigest, State: record.Job.State, Stage: record.Job.Stage, Attempt: record.Job.Attempt, Progress: record.Job.Progress, RowVersion: record.Job.RowVersion, LeaseUntil: record.Job.LeaseUntil, Error: record.Job.Error}
}

func idValue(id *contracts.ID) contracts.ID {
	if id == nil {
		return ""
	}
	return *id
}

func deletionJob(ctx context.Context, db *sql.DB, space, kb, documentID contracts.ID) (*webJob, error) {
	if db == nil || documentID == "" {
		return nil, errors.New("invalid deletion job request")
	}
	var id, raw, state, attempt, version string
	var profile contracts.Digest
	var progress float64
	err := db.QueryRowContext(ctx, `SELECT j.id,j.profile_digest,j.job,p.state,p.attempt::text,CASE WHEN p.total_targets=0 AND p.state='completed' THEN 1 ELSE COALESCE(p.completed_targets::double precision/NULLIF(p.total_targets,0),0) END,p.row_version::text
FROM akos_withdraw_v1.document_delete_jobs j
JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id
WHERE j.space_id=$1 AND j.kb_id=$2 AND j.document_id=$3
ORDER BY j.id DESC LIMIT 1`, space, kb, documentID).Scan(&id, &profile, &raw, &state, &attempt, &progress, &version)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil || len(raw) == 0 || len(raw) > 65536 {
		return nil, errors.New("deletion job unavailable")
	}
	var value contracts.Job
	if json.Unmarshal([]byte(raw), &value) != nil || value.ID != contracts.ID(id) || value.SpaceID != space || value.ProfileDigest != profile || value.OperationID == "" || value.Stage != "document.delete.cleanup" || value.State != "queued" || value.RowVersion != 1 {
		return nil, errors.New("deletion job unavailable")
	}
	var outAttempt, outVersion uint64
	if _, err := fmt.Sscan(attempt, &outAttempt); err != nil {
		return nil, errors.New("deletion job unavailable")
	}
	if _, err := fmt.Sscan(version, &outVersion); err != nil {
		return nil, errors.New("deletion job unavailable")
	}
	return &webJob{ID: value.ID, OperationID: value.OperationID, ProfileDigest: value.ProfileDigest, State: state, Stage: value.Stage, Attempt: outAttempt, Progress: progress, RowVersion: outVersion, LeaseUntil: value.LeaseUntil, Error: value.Error}, nil
}

func publicationJob(ctx context.Context, db *sql.DB, space, kb, draft contracts.ID) (*webJob, error) {
	if db == nil || draft == "" {
		return nil, errors.New("invalid publication job request")
	}
	var operation, job, revision string
	var payload []byte
	var history sql.NullString
	var createdAt time.Time
	err := db.QueryRowContext(ctx, `SELECT i.id,i.job_id,i.revision_id,h.payload,f.history_id,a.occurred_at
FROM akos_withdraw_v1.submission_receipts r
JOIN akos_withdraw_v1.publication_intents i ON i.space_id=r.space_id AND i.kb_id=r.kb_id AND i.revision_id=r.revision_id
JOIN akos_withdraw_v1.publication_task_heads h ON h.space_id=i.space_id AND h.kb_id=i.kb_id AND h.id=i.job_id
LEFT JOIN akos_withdraw_v1.publication_finalizations f ON f.space_id=i.space_id AND f.kb_id=i.kb_id AND f.operation_id=i.id
JOIN akos_withdraw_v1.publication_start_audit a ON a.space_id=i.space_id AND a.kb_id=i.kb_id AND a.operation_id=i.id
WHERE r.space_id=$1 AND r.kb_id=$2 AND r.draft_id=$3
ORDER BY i.expected_epoch DESC,i.id DESC LIMIT 1`, space, kb, draft).Scan(&operation, &job, &revision, &payload, &history, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil || len(payload) == 0 || len(payload) > 32768 {
		return nil, errors.New("publication job unavailable")
	}
	var value contracts.Job
	if json.Unmarshal(payload, &value) != nil || value.ID != contracts.ID(job) || value.OperationID != contracts.ID(operation) || value.RevisionID == nil || *value.RevisionID != contracts.ID(revision) {
		return nil, errors.New("publication job unavailable")
	}
	result := &webJob{ID: value.ID, OperationID: value.OperationID, RevisionID: *value.RevisionID, State: value.State, Stage: value.Stage, Attempt: value.Attempt, Progress: value.Progress, RowVersion: value.RowVersion, LeaseUntil: value.LeaseUntil, Error: value.Error, CreatedAt: createdAt.UTC()}
	if history.Valid {
		result.HistoryID = contracts.ID(history.String)
	}
	return result, nil
}

func publicationTaskList(ctx context.Context, db *sql.DB, space, kb contracts.ID) ([]webTask, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.draft_id,i.document_id,i.revision_id,rc.content_hash,i.id,i.job_id,h.payload,f.history_id,a.occurred_at
FROM akos_withdraw_v1.submission_receipts r
JOIN akos_withdraw_v1.publication_intents i ON i.space_id=r.space_id AND i.kb_id=r.kb_id AND i.revision_id=r.revision_id
JOIN akos_withdraw_v1.revision_contents rc ON rc.space_id=i.space_id AND rc.kb_id=i.kb_id AND rc.document_id=i.document_id AND rc.revision_id=i.revision_id
JOIN akos_withdraw_v1.publication_task_heads h ON h.space_id=i.space_id AND h.kb_id=i.kb_id AND h.id=i.job_id
LEFT JOIN akos_withdraw_v1.publication_finalizations f ON f.space_id=i.space_id AND f.kb_id=i.kb_id AND f.operation_id=i.id
JOIN akos_withdraw_v1.publication_start_audit a ON a.space_id=i.space_id AND a.kb_id=i.kb_id AND a.operation_id=i.id
WHERE r.space_id=$1 AND r.kb_id=$2 ORDER BY i.id DESC LIMIT 101`, space, kb)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	tasks := make([]webTask, 0, 100)
	for rows.Next() {
		var draft, document, revision, operation, job string
		var contentHash contracts.Digest
		var payload []byte
		var history sql.NullString
		var createdAt time.Time
		if rows.Scan(&draft, &document, &revision, &contentHash, &operation, &job, &payload, &history, &createdAt) != nil || len(payload) == 0 || len(payload) > 32768 {
			return nil, false, errors.New("publication task unavailable")
		}
		var value contracts.Job
		if json.Unmarshal(payload, &value) != nil || value.ID != contracts.ID(job) || value.OperationID != contracts.ID(operation) || value.RevisionID == nil || *value.RevisionID != contracts.ID(revision) {
			return nil, false, errors.New("publication task unavailable")
		}
		item := &webJob{ID: value.ID, OperationID: value.OperationID, RevisionID: *value.RevisionID, State: value.State, Stage: value.Stage, Attempt: value.Attempt, Progress: value.Progress, RowVersion: value.RowVersion, LeaseUntil: value.LeaseUntil, Error: value.Error, CreatedAt: createdAt.UTC()}
		if history.Valid {
			item.HistoryID = contracts.ID(history.String)
		}
		tasks = append(tasks, webTask{Kind: "publication", DocumentID: contracts.ID(document), DraftID: contracts.ID(draft), RevisionID: contracts.ID(revision), ContentHash: contentHash, Job: item})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return tasks, len(tasks) == 100, nil
}

func deletionTaskList(ctx context.Context, db *sql.DB, space, kb contracts.ID) ([]webTask, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT j.document_id,j.id,j.profile_digest,j.job,p.state,p.attempt::text,
CASE WHEN p.total_targets=0 AND p.state='completed' THEN 1 ELSE COALESCE(p.completed_targets::double precision/NULLIF(p.total_targets,0),0) END,p.row_version::text,h.occurred_at
FROM akos_withdraw_v1.document_delete_jobs j JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id JOIN akos_withdraw_v1.publication_history h ON h.space_id=j.space_id AND h.kb_id=j.kb_id AND h.document_id=j.document_id AND h.id=j.history_id
WHERE j.space_id=$1 AND j.kb_id=$2 ORDER BY j.id DESC LIMIT 101`, space, kb)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	tasks := make([]webTask, 0, 100)
	for rows.Next() {
		var document, id, raw, state, attempt, version string
		var profile contracts.Digest
		var progress float64
		var createdAt time.Time
		if rows.Scan(&document, &id, &profile, &raw, &state, &attempt, &progress, &version, &createdAt) != nil || len(raw) == 0 || len(raw) > 65536 {
			return nil, false, errors.New("deletion task unavailable")
		}
		var value contracts.Job
		if json.Unmarshal([]byte(raw), &value) != nil || value.ID != contracts.ID(id) || value.SpaceID != space || value.ProfileDigest != profile || value.OperationID == "" || value.Stage != "document.delete.cleanup" || value.State != "queued" || value.RowVersion != 1 {
			return nil, false, errors.New("deletion task unavailable")
		}
		outAttempt, e1 := strconv.ParseUint(attempt, 10, 64)
		outVersion, e2 := strconv.ParseUint(version, 10, 64)
		if e1 != nil || e2 != nil {
			return nil, false, errors.New("deletion task unavailable")
		}
		tasks = append(tasks, webTask{Kind: "deletion", DocumentID: contracts.ID(document), Job: &webJob{ID: value.ID, OperationID: value.OperationID, State: state, Stage: value.Stage, Attempt: outAttempt, Progress: progress, RowVersion: outVersion, LeaseUntil: value.LeaseUntil, Error: value.Error, CreatedAt: createdAt.UTC()}})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return tasks, len(tasks) == 100, nil
}

func (h *webHost) jobStatus(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return errAuthority
	}
	draft := contracts.ID(r.PathValue("draft"))
	if draft == "" {
		return errors.New("缺少草稿标识。")
	}
	job, err := publicationJob(ctx, h.db, h.s.m.Space, h.s.m.KB, draft)
	if err != nil {
		return errDatabase
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"draft_id": draft, "job": job})
}

func (h *webHost) jobsList(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	if a == nil || h.s == nil || h.db == nil {
		return errAuthority
	}
	read, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return errAuthority
	}
	cleanup, err := a.scope(ctx, "publication.cleanup.read", "")
	if err != nil {
		return errAuthority
	}
	filter, err := parseTaskListFilter(r)
	if err != nil {
		return err
	}
	snapshot, err := h.taskSnapshot(ctx)
	if err != nil {
		return errDatabase
	}
	if a.verify(ctx, read) != nil || a.verify(ctx, cleanup) != nil {
		return errAuthority
	}
	if tasks, ok := snapshot["tasks"].([]webTask); ok {
		snapshot["tasks"] = filterTasks(tasks, filter)
	}
	return webJSON(w, snapshot)
}

// taskSnapshot is shared by the JSON task list and the SSE stream. It keeps
// the stream payload identical to the normal task endpoint and omits worker
// credentials and internal fencing fields.
func (h *webHost) taskSnapshot(ctx context.Context) (map[string]any, error) {
	publications, morePublications, err := publicationTaskList(ctx, h.db, h.s.m.Space, h.s.m.KB)
	if err != nil {
		return nil, err
	}
	deletions, moreDeletions, err := deletionTaskList(ctx, h.db, h.s.m.Space, h.s.m.KB)
	if err != nil {
		return nil, err
	}
	parsing := h.parseTasksForList()
	tasks := append(publications, deletions...)
	tasks = append(tasks, parsing...)
	return map[string]any{"tasks": tasks, "has_more": morePublications || moreDeletions}, nil
}

func (h *webHost) parseTasksForList() []webTask {
	h.parseMu.Lock()
	defer h.parseMu.Unlock()
	items := make([]webTask, 0, len(h.parseTasks))
	for _, task := range h.parseTasks {
		var documentID contracts.ID
		if task.Result != nil {
			documentID = task.Result.DocumentID
		}
		var failure *contracts.PublicError
		if task.Error != "" {
			failure = &contracts.PublicError{Code: "parse_failed", SafeMessage: task.Error, Retryable: task.State == "failed"}
		}
		items = append(items, webTask{Kind: "parse", DocumentID: documentID, Job: &webJob{ID: task.ID, State: task.State, Stage: task.Stage, Attempt: task.Attempt, RowVersion: task.RowVersion, Progress: map[bool]float64{true: 1, false: 0}[task.State == "completed"], Error: failure, CreatedAt: task.CreatedAt.UTC()}})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Job.ID > items[j].Job.ID })
	return items
}

// jobEvents provides a bounded, authenticated SSE view of task progress. The
// durable task rows remain authoritative; the stream only reduces UI polling
// latency and closes automatically when the request context expires.
func (h *webHost) jobEvents(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	if a == nil || h.s == nil || h.db == nil {
		return errAuthority
	}
	read, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return errAuthority
	}
	cleanup, err := a.scope(ctx, "publication.cleanup.read", "")
	if err != nil {
		return errAuthority
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("任务事件流不可用")
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Event-Snapshot", "full")
	var previous []byte
	sequence := lastEventID(r)
	_, _ = fmt.Fprint(w, "retry: 1000\n\n")
	flusher.Flush()
	send := func(force bool) error {
		if a.verify(ctx, read) != nil || a.verify(ctx, cleanup) != nil {
			return context.Canceled
		}
		snapshot, e := h.taskSnapshot(ctx)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(snapshot)
		if e != nil {
			return e
		}
		if !force && bytes.Equal(previous, raw) {
			return nil
		}
		previous = append(previous[:0], raw...)
		sequence++
		_, e = fmt.Fprintf(w, "id: %d\nevent: tasks\ndata: %s\n\n", sequence, raw)
		if e == nil {
			flusher.Flush()
		}
		return e
	}
	if err := send(true); err != nil {
		return nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := send(false); err != nil {
				return nil
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		}
	}
}

// lastEventID accepts the browser's reconnect cursor without treating it as
// authorization or a task cursor. Each event is a complete snapshot, so a
// reconnect only needs monotonic numbering; the server does not replay old
// internal task rows.
func lastEventID(r *http.Request) uint64 {
	if r == nil {
		return 0
	}
	value, err := strconv.ParseUint(strings.TrimSpace(r.Header.Get("Last-Event-ID")), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func (h *webHost) deleteJobStatus(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	documentID := contracts.ID(r.PathValue("document"))
	if documentID == "" {
		return errors.New("缺少文档标识。")
	}
	if _, err := a.scope(ctx, "document.delete", documentID); err != nil {
		return errAuthority
	}
	job, err := deletionJob(ctx, h.db, h.s.m.Space, h.s.m.KB, documentID)
	if err != nil {
		return errDatabase
	}
	return webJSON(w, map[string]any{"document_id": documentID, "job": job})
}

type cleanupPlanStatus struct {
	ID         contracts.ID `json:"plan_id"`
	RevisionID contracts.ID `json:"revision_id"`
	Action     string       `json:"action"`
	State      string       `json:"state"`
	Total      uint64       `json:"total"`
	Completed  uint64       `json:"completed"`
	Failed     uint64       `json:"failed"`
	Progress   float64      `json:"progress"`
	CreatedAt  time.Time    `json:"created_at"`
}

func cleanupPlan(ctx context.Context, db *sql.DB, space, kb, documentID contracts.ID) (*cleanupPlanStatus, error) {
	var value cleanupPlanStatus
	var created, total, completed, failed string
	err := db.QueryRowContext(ctx, `SELECT p.id,p.revision_id,p.history_action,p.created_at::text,
count(x.target_id)::text,count(x.target_id) FILTER (WHERE x.state='succeeded')::text,
count(x.target_id) FILTER (WHERE x.state IN('failed','unknown'))::text
FROM akos_withdraw_v1.cleanup_plans p
JOIN akos_withdraw_v1.cleanup_progress x ON x.plan_id=p.id
WHERE p.space_id=$1 AND p.kb_id=$2 AND p.document_id=$3
GROUP BY p.id,p.revision_id,p.history_action,p.created_at,p.publication_epoch
ORDER BY p.publication_epoch DESC LIMIT 1`, space, kb, documentID).Scan(&value.ID, &value.RevisionID, &value.Action, &created, &total, &completed, &failed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("cleanup status unavailable")
	}
	var errParse error
	var totalN, completedN, failedN uint64
	totalN, errParse = strconv.ParseUint(total, 10, 64)
	if errParse != nil {
		return nil, errors.New("cleanup status unavailable")
	}
	completedN, errParse = strconv.ParseUint(completed, 10, 64)
	if errParse != nil {
		return nil, errors.New("cleanup status unavailable")
	}
	failedN, errParse = strconv.ParseUint(failed, 10, 64)
	if errParse != nil {
		return nil, errors.New("cleanup status unavailable")
	}
	value.Total, value.Completed, value.Failed = totalN, completedN, failedN
	if totalN > 0 {
		value.Progress = float64(completedN) / float64(totalN)
	}
	if failedN > 0 {
		value.State = "failed"
	} else if completedN == totalN && totalN > 0 {
		value.State = "completed"
	} else if completedN > 0 {
		value.State = "running"
	} else {
		value.State = "queued"
	}
	if parsed, e := time.Parse("2006-01-02T15:04:05.000000000Z", created); e == nil {
		value.CreatedAt = parsed.UTC()
	} else {
		return nil, errors.New("cleanup status unavailable")
	}
	return &value, nil
}

func (h *webHost) cleanupStatus(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	documentID := contracts.ID(r.PathValue("document"))
	if documentID == "" {
		return errors.New("缺少文档标识。")
	}
	if a == nil || h.s == nil || h.db == nil {
		return errAuthority
	}
	scope, err := a.scope(ctx, "publication.cleanup.read", documentID)
	if err != nil {
		return errAuthority
	}
	plan, err := cleanupPlan(ctx, h.db, h.s.m.Space, h.s.m.KB, documentID)
	if err != nil {
		return errDatabase
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"document_id": documentID, "plan": plan})
}

func (h *webHost) controlDeleteJob(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority, retry bool) error {
	var q struct {
		DocumentID         contracts.ID `json:"document_id"`
		JobID              contracts.ID `json:"job_id"`
		ExpectedJobVersion uint64       `json:"expect_version"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if q.DocumentID == "" || q.JobID == "" || q.ExpectedJobVersion == 0 {
		return errors.New("请提供文档、清理任务和当前任务版本。")
	}
	if a == nil || h.s == nil || h.db == nil {
		return errAuthority
	}
	job, err := deletionJob(ctx, h.db, h.s.m.Space, h.s.m.KB, q.DocumentID)
	if err != nil || job == nil || job.ID != q.JobID {
		return errors.New("清理任务不存在或已变更，请刷新后重试。")
	}
	scope, err := a.scope(ctx, "publication.cleanup.control", q.DocumentID)
	if err != nil {
		return errAuthority
	}
	authorize := func(c context.Context, current contracts.AccessScope, value contracts.ResourceJob) error {
		if value.ResourceID != q.DocumentID || value.KnowledgeBaseID != h.s.m.KB || value.Job.ID != q.JobID || value.Job.SpaceID != h.s.m.Space || value.Job.ProfileDigest != job.ProfileDigest {
			return errAuthority
		}
		return a.verify(c, current)
	}
	store := postgres.DeletionCleanupJobs{DB: h.db, Verify: a.verify, AuthorizeControl: authorize}
	deadline, ok := ctx.Deadline()
	if !ok {
		return errAuthority
	}
	input := contracts.DeletionCleanupControlRequest{Request: contracts.RequestContext{RequestID: contracts.ID("local-cleanup-control-" + nonce()), ProfileDigest: h.s.m.ProfileDigest, Deadline: deadline}, Scope: scope, JobID: q.JobID, ExpectedJobVersion: q.ExpectedJobVersion}
	var result contracts.ResourceJob
	if retry {
		result, err = store.RetryDeletionCleanup(ctx, input)
	} else {
		result, err = store.CancelDeletionCleanup(ctx, input)
	}
	if err != nil {
		return errors.New("清理任务当前不能执行该控制操作，请刷新状态后重试。")
	}
	return webJSON(w, map[string]any{"document_id": q.DocumentID, "job": projectWebJob(result)})
}

func (h *webHost) retryDeleteJob(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	return h.controlDeleteJob(ctx, w, r, a, true)
}

func (h *webHost) cancelDeleteJob(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	return h.controlDeleteJob(ctx, w, r, a, false)
}

func (h *webHost) retryDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DraftID contracts.ID     `json:"draft_id"`
		Hash    contracts.Digest `json:"expect_hash"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if q.DraftID == "" || len(q.Hash) != 64 {
		return errors.New("请先核对全文和内容摘要。")
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	result, err := h.s.publishDraft(ctx, h.db, a, q.DraftID, q.Hash, nil, true)
	if err != nil {
		return err
	}
	return webJSON(w, map[string]any{"state": "published", "document_id": result.Operation.DocumentID, "revision_id": result.Operation.RevisionID})
}

func (h *webHost) cancelPublication(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DraftID            contracts.ID `json:"draft_id"`
		JobID              contracts.ID `json:"job_id"`
		ExpectedJobVersion uint64       `json:"expect_version"`
		Action             string       `json:"action"`
	}
	if err := readWebJSON(w, r, &q); err != nil || q.DraftID == "" || q.JobID == "" || q.ExpectedJobVersion == 0 {
		return errors.New("请提供草稿、发布任务和当前任务版本。")
	}
	job, err := publicationJob(ctx, h.db, h.s.m.Space, h.s.m.KB, q.DraftID)
	if err != nil || job == nil || job.ID != q.JobID || job.RowVersion != q.ExpectedJobVersion || (job.State != "queued" && job.State != "running") {
		return errors.New("发布任务不存在、已完成或已变更，请刷新后重试。")
	}
	var documentID contracts.ID
	if err := h.db.QueryRowContext(ctx, `SELECT document_id FROM akos_withdraw_v1.publication_intents WHERE space_id=$1 AND kb_id=$2 AND job_id=$3`, h.s.m.Space, h.s.m.KB, q.JobID).Scan(&documentID); err != nil || documentID == "" {
		return errors.New("发布任务所属文档不可用。")
	}
	scope, err := a.scope(ctx, "publication.publish", documentID)
	if err != nil {
		return errAuthority
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return errDatabase
	}
	defer tx.Rollback()
	var raw []byte
	var state, version, attempt, fencing string
	var workerID, attemptID, leaseUntil sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT payload,state,row_version::text,attempt::text,fencing_token::text,worker_id,attempt_id,lease_until FROM akos_withdraw_v1.publication_task_heads WHERE space_id=$1 AND kb_id=$2 AND id=$3 FOR UPDATE`, h.s.m.Space, h.s.m.KB, q.JobID).Scan(&raw, &state, &version, &attempt, &fencing, &workerID, &attemptID, &leaseUntil); err != nil || len(raw) == 0 {
		return errors.New("发布任务不存在或已变更，请刷新后重试。")
	}
	var current contracts.Job
	if json.Unmarshal(raw, &current) != nil || current.ID != q.JobID || current.State != state || strconv.FormatUint(current.RowVersion, 10) != version || strconv.FormatUint(current.Attempt, 10) != attempt || strconv.FormatUint(current.FencingToken, 10) != fencing || current.RowVersion != q.ExpectedJobVersion || (state != "queued" && state != "running") {
		return errors.New("发布任务已变更，请刷新后重试。")
	}
	action := q.Action
	if action == "" {
		action = "cancel"
	}
	if action != "cancel" && action != "pause" {
		return errors.New("发布任务控制操作无效。")
	}
	targetState, code, message := "failed", contracts.Code("cancelled"), "发布任务已取消。"
	retryable := false
	if action == "pause" {
		targetState, code, message, retryable = "paused", contracts.Code("paused"), "发布任务已暂停，可继续恢复。", true
	}
	current.State = targetState
	if current.RowVersion == ^uint64(0) || current.FencingToken == ^uint64(0) {
		return errors.New("发布任务版本已耗尽。")
	}
	current.RowVersion++
	current.FencingToken++
	current.LeaseUntil = nil
	current.Error = &contracts.PublicError{Code: code, SafeMessage: message, RequestID: contracts.ID("local-publication-"+action) + contracts.ID(nonce()), Retryable: retryable}
	jobBytes, err := json.Marshal(current)
	if err != nil {
		return errDatabase
	}
	resultID := contracts.ID("local-publication-" + action + "-" + nonce())
	requestID := contracts.ID("local-publication-" + action + "-" + nonce())
	requestDigest := digest([]byte(string(h.s.m.ProfileDigest) + "\x00" + string(q.JobID) + "\x00" + string(resultID)))
	resultBytes, err := json.Marshal(map[string]any{"JobVersion": current.RowVersion, "Lease": map[string]any{"JobID": "", "WorkerID": "", "AttemptID": "", "FencingToken": 0}})
	if err != nil {
		return errDatabase
	}
	args := []any{h.s.m.Space, h.s.m.KB, q.JobID, strconv.FormatUint(current.RowVersion, 10), string(current.State), strconv.FormatUint(current.Attempt, 10), strconv.FormatUint(current.FencingToken, 10), nil, nil, nil, string(jobBytes), strconv.FormatUint(q.ExpectedJobVersion, 10)}
	if _, err = tx.ExecContext(ctx, `UPDATE akos_withdraw_v1.publication_task_heads SET row_version=$4::numeric,state=$5,attempt=$6::numeric,fencing_token=$7::numeric,worker_id=$8,attempt_id=$9,lease_until=$10,payload=$11::jsonb WHERE space_id=$1 AND kb_id=$2 AND id=$3 AND row_version=$12::numeric`, args...); err != nil {
		return errors.New("发布任务取消失败，请稍后重试。")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_task_receipts(space_id,kb_id,job_id,worker_id,kind,command_id,request_id,request_digest,status,job_version,result) VALUES($1,$2,$3,$4,'claim',$5,$6,$7,'superseded',$8::numeric,$9::jsonb)`, h.s.m.Space, h.s.m.KB, q.JobID, h.s.m.Owner, resultID, requestID, requestDigest, strconv.FormatUint(current.RowVersion, 10), string(resultBytes)); err != nil {
		return errors.New("发布任务取消记录失败，请稍后重试。")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_task_audit(space_id,kb_id,job_id,job_version,worker_id,kind,command_id,request_id,payload) VALUES($1,$2,$3,$4::numeric,$5,'claim',$6,$7,$8::jsonb)`, h.s.m.Space, h.s.m.KB, q.JobID, strconv.FormatUint(current.RowVersion, 10), h.s.m.Owner, resultID, requestID, string(jobBytes)); err != nil {
		return errors.New("发布任务审计记录失败，请稍后重试。")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.publication_task_outbox(space_id,kb_id,job_id,job_version,kind,payload) VALUES($1,$2,$3,$4::numeric,'claim',$5::jsonb)`, h.s.m.Space, h.s.m.KB, q.JobID, strconv.FormatUint(current.RowVersion, 10), string(jobBytes)); err != nil {
		return errors.New("发布任务事件记录失败，请稍后重试。")
	}
	if a.verify(ctx, scope) != nil || tx.Commit() != nil {
		return errors.New("发布任务取消结果不确定，请刷新状态后重试。")
	}
	return webJSON(w, map[string]any{"draft_id": q.DraftID, "document_id": documentID, "job": projectWebJob(currentResourceJob(current, documentID, h.s.m.KB, h.s.m.Space))})
}

func currentResourceJob(job contracts.Job, documentID, kb, space contracts.ID) contracts.ResourceJob {
	return contracts.ResourceJob{ResourceID: documentID, KnowledgeBaseID: kb, Job: job}
}
