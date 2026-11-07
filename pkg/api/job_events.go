package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// JobEventsHandler streams complete, authorization-checked job snapshots. It
// keeps no replay buffer: Last-Event-ID only continues the event sequence.
type JobEventsHandler struct{ Service JobStatusApplication }

func (h JobEventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	if h.Service == nil || r.URL.RawQuery != "" {
		http.Error(w, "job events unavailable", http.StatusServiceUnavailable)
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	token, ok := strings.CutPrefix(headers[0], "Bearer ")
	if !ok || token == "" || strings.ContainsAny(token, " \t\r\n,") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	jobID := contracts.ID(r.PathValue("id"))
	if !partnerID(jobID) {
		http.Error(w, "Invalid job request", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Job events unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	sequence := parseEventID(r.Header.Get("Last-Event-ID"))
	var previous []byte
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = io.WriteString(w, "retry: 1000\n\n")
	flusher.Flush()
	send := func(force bool) error {
		requestID := contracts.ID(hex.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), 10))))
		record, err := h.Service.GetJob(ctx, document.JobStatusInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, Deadline: deadline}, JobID: jobID})
		if err != nil || record.Job.ID != jobID {
			return err
		}
		body, err := json.Marshal(jobEventProjection(record))
		if err != nil {
			return err
		}
		if !force && string(body) == string(previous) {
			return nil
		}
		previous = append(previous[:0], body...)
		sequence++
		if _, err = io.WriteString(w, "id: "+strconv.FormatUint(sequence, 10)+"\nevent: job\ndata: "+string(body)+"\n\n"); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	if err := send(true); err != nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := send(false); err != nil {
				return
			}
		}
	}
}

func parseEventID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func jobEventProjection(record contracts.ResourceJob) map[string]any {
	job := record.Job
	result := map[string]any{"job_id": job.ID, "resource_id": record.ResourceID, "state": job.State, "stage": job.Stage, "attempt": job.Attempt, "row_version": job.RowVersion, "progress": job.Progress}
	if job.Error != nil {
		result["error"] = map[string]any{"code": job.Error.Code, "safe_message": job.Error.SafeMessage, "retryable": job.Error.Retryable, "request_id": job.Error.RequestID}
	}
	return result
}
