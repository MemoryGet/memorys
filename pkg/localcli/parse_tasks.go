package localcli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

const parseTaskFile = "parse-tasks.json"

type parseTask struct {
	ID         contracts.ID  `json:"task_id"`
	State      string        `json:"state"`
	Stage      string        `json:"stage"`
	Attempt    uint64        `json:"attempt"`
	RowVersion uint64        `json:"row_version"`
	Error      string        `json:"error,omitempty"`
	Result     *importResult `json:"result,omitempty"`
	InputPath  string        `json:"input_path"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type parseTaskPublic struct {
	ID         contracts.ID  `json:"task_id"`
	State      string        `json:"state"`
	Stage      string        `json:"stage"`
	Attempt    uint64        `json:"attempt"`
	RowVersion uint64        `json:"row_version"`
	Error      string        `json:"error,omitempty"`
	Result     *importResult `json:"result,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

func publicParseTask(value parseTask) parseTaskPublic {
	return parseTaskPublic{ID: value.ID, State: value.State, Stage: value.Stage, Attempt: value.Attempt, RowVersion: value.RowVersion, Error: value.Error, Result: value.Result, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func loadParseTasks(root string) (map[contracts.ID]parseTask, error) {
	tasks := map[contracts.ID]parseTask{}
	path := filepath.Join(root, parseTaskFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return tasks, nil
	}
	if err != nil || len(raw) > 4<<20 {
		return nil, errState
	}
	var values []parseTask
	if json.Unmarshal(raw, &values) != nil || len(values) > 10000 {
		return nil, errState
	}
	for _, value := range values {
		if value.ID == "" || value.RowVersion == 0 || value.Attempt == 0 || value.InputPath == "" {
			return nil, errState
		}
		if !taskPathInsideRoot(root, value.InputPath) {
			return nil, errState
		}
		if value.State == "running" || value.State == "queued" {
			value.State = "failed"
			value.Error = "解析任务在进程重启期间未完成，可重试。"
			value.RowVersion++
			value.UpdatedAt = time.Now().UTC()
		}
		tasks[value.ID] = value
	}
	return tasks, nil
}

func taskPathInsideRoot(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func persistParseTasks(root string, tasks map[contracts.ID]parseTask) error {
	values := make([]parseTask, 0, len(tasks))
	for _, value := range tasks {
		values = append(values, value)
	}
	raw, err := json.Marshal(values)
	if err != nil || len(raw) > 4<<20 {
		return errState
	}
	return writeAtomic(root, parseTaskFile, raw)
}

func (h *webHost) createParseTask(ext string) (parseTask, error) {
	if h == nil || h.s == nil || h.s.root == "" {
		return parseTask{}, errState
	}
	id := contracts.ID("parse-" + nonce())
	now := time.Now().UTC()
	task := parseTask{ID: id, State: "queued", Stage: "document.parse", Attempt: 1, RowVersion: 1, InputPath: filepath.Join(h.s.root, ".parse-input-"+string(id)+ext), CreatedAt: now, UpdatedAt: now}
	h.parseMu.Lock()
	defer h.parseMu.Unlock()
	h.parseTasks[id] = task
	if err := persistParseTasks(h.s.root, h.parseTasks); err != nil {
		delete(h.parseTasks, id)
		return parseTask{}, err
	}
	return task, nil
}

func (h *webHost) executeParseTask(ctx context.Context, a *authority, id contracts.ID) <-chan struct{} {
	done := make(chan struct{})
	child, cancel := context.WithCancel(ctx)
	h.parseMu.Lock()
	task, ok := h.parseTasks[id]
	if ok {
		task.State, task.Stage, task.UpdatedAt = "running", "document.parse", time.Now().UTC()
		task.RowVersion++
		h.parseTasks[id] = task
		h.parseCancels[id] = cancel
		_ = persistParseTasks(h.s.root, h.parseTasks)
	}
	h.parseMu.Unlock()
	go func() {
		defer close(done)
		if !ok {
			cancel()
			return
		}
		h.mutations.Lock()
		result, err := h.s.importFile(child, h.db, a, task.InputPath)
		h.mutations.Unlock()
		cancel()
		h.parseMu.Lock()
		defer h.parseMu.Unlock()
		current, present := h.parseTasks[id]
		if present {
			if current.State == "paused" {
				current.Error = "解析任务已暂停，可继续恢复。"
			} else if child.Err() != nil && err != nil {
				current.State, current.Error = "cancelled", "解析任务已取消。"
			} else if err != nil {
				current.State, current.Error = "failed", safeParseTaskError(err)
			} else {
				current.State, current.Error, current.Result = "completed", "", &result
				_ = os.Remove(current.InputPath)
			}
			current.RowVersion++
			current.UpdatedAt = time.Now().UTC()
			h.parseTasks[id] = current
			delete(h.parseCancels, id)
			_ = persistParseTasks(h.s.root, h.parseTasks)
		}
	}()
	return done
}

func safeParseTaskError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "解析任务已取消或超时，可重试。"
	}
	return "解析任务失败，可重试。"
}

func (h *webHost) parseTaskByID(id contracts.ID) (parseTask, bool) {
	h.parseMu.Lock()
	defer h.parseMu.Unlock()
	task, ok := h.parseTasks[id]
	return task, ok
}

func (h *webHost) cancelParseTask(id contracts.ID, expected uint64) (parseTask, error) {
	h.parseMu.Lock()
	defer h.parseMu.Unlock()
	task, ok := h.parseTasks[id]
	if !ok || task.RowVersion != expected || (task.State != "queued" && task.State != "running") {
		return parseTask{}, errors.New("解析任务不存在、已完成或已变更，请刷新后重试。")
	}
	if cancel := h.parseCancels[id]; cancel != nil {
		cancel()
	}
	if task.State == "queued" {
		task.State = "cancelled"
		task.Error = "解析任务已取消。"
	}
	task.RowVersion++
	task.UpdatedAt = time.Now().UTC()
	h.parseTasks[id] = task
	if err := persistParseTasks(h.s.root, h.parseTasks); err != nil {
		return parseTask{}, errState
	}
	return task, nil
}

func (h *webHost) pauseParseTask(id contracts.ID, expected uint64) (parseTask, error) {
	h.parseMu.Lock()
	defer h.parseMu.Unlock()
	task, ok := h.parseTasks[id]
	if !ok || task.RowVersion != expected || (task.State != "queued" && task.State != "running") {
		return parseTask{}, errors.New("解析任务不存在、已完成或已变更，请刷新后重试。")
	}
	if cancel := h.parseCancels[id]; cancel != nil {
		cancel()
	}
	task.State = "paused"
	task.Error = "解析任务已暂停，可继续恢复。"
	task.RowVersion++
	task.UpdatedAt = time.Now().UTC()
	h.parseTasks[id] = task
	if err := persistParseTasks(h.s.root, h.parseTasks); err != nil {
		return parseTask{}, errState
	}
	return task, nil
}

func (h *webHost) retryParseTask(ctx context.Context, a *authority, id contracts.ID, expected uint64) (parseTask, <-chan struct{}, error) {
	h.parseMu.Lock()
	task, ok := h.parseTasks[id]
	if !ok || task.RowVersion != expected || (task.State != "failed" && task.State != "cancelled" && task.State != "paused") || task.InputPath == "" {
		h.parseMu.Unlock()
		return parseTask{}, nil, errors.New("解析任务不存在、已完成或已变更，请刷新后重试。")
	}
	task.Attempt++
	task.RowVersion++
	task.State, task.Error, task.Result, task.UpdatedAt = "queued", "", nil, time.Now().UTC()
	h.parseTasks[id] = task
	if err := persistParseTasks(h.s.root, h.parseTasks); err != nil {
		h.parseMu.Unlock()
		return parseTask{}, nil, errState
	}
	h.parseMu.Unlock()
	return task, h.executeParseTask(ctx, a, id), nil
}

func (h *webHost) pauseParseJob(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		TaskID          contracts.ID `json:"task_id"`
		ExpectedVersion uint64       `json:"expect_version"`
	}
	if err := readWebJSON(w, r, &q); err != nil || q.TaskID == "" || q.ExpectedVersion == 0 {
		return errors.New("请提供解析任务和当前任务版本。")
	}
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	task, err := h.pauseParseTask(q.TaskID, q.ExpectedVersion)
	if err != nil {
		return err
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"task": publicParseTask(task)})
}

func (h *webHost) parseJobStatus(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	task, ok := h.parseTaskByID(contracts.ID(r.PathValue("id")))
	if !ok {
		return errors.New("解析任务不存在。")
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"task": publicParseTask(task)})
}

func (h *webHost) cancelParseJob(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		TaskID          contracts.ID `json:"task_id"`
		ExpectedVersion uint64       `json:"expect_version"`
	}
	if err := readWebJSON(w, r, &q); err != nil || q.TaskID == "" || q.ExpectedVersion == 0 {
		return errors.New("请提供解析任务和当前任务版本。")
	}
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	task, err := h.cancelParseTask(q.TaskID, q.ExpectedVersion)
	if err != nil {
		return err
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"task": publicParseTask(task)})
}

func (h *webHost) retryParseJob(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		TaskID          contracts.ID `json:"task_id"`
		ExpectedVersion uint64       `json:"expect_version"`
	}
	if err := readWebJSON(w, r, &q); err != nil || q.TaskID == "" || q.ExpectedVersion == 0 {
		return errors.New("请提供解析任务和当前任务版本。")
	}
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	_, done, err := h.retryParseTask(ctx, a, q.TaskID, q.ExpectedVersion)
	if err != nil {
		return err
	}
	<-done
	task, ok := h.parseTaskByID(q.TaskID)
	if !ok {
		return errDatabase
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"task": publicParseTask(task)})
}
