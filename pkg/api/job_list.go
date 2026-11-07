package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type JobListApplication = contracts.JobListApplication

type JobListHandler struct{ Service JobListApplication }

func (h JobListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := requestNonce(w)
	fail := func(status int, code, message string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "GET required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Job list not configured")
		return
	}
	q := r.URL.Query()
	for key := range q {
		switch key {
		case "kind", "state", "document_id", "created_after", "created_before", "cursor", "page_size":
		default:
			fail(http.StatusBadRequest, "invalid_argument", "Unknown query parameter")
			return
		}
	}
	kind, state, documentID := q.Get("kind"), q.Get("state"), contracts.ID(q.Get("document_id"))
	if len(q["kind"]) > 1 || len(q["state"]) > 1 || len(q["document_id"]) > 1 || len(q["created_after"]) > 1 || len(q["created_before"]) > 1 || len(q["cursor"]) > 1 || kind != "" && kind != "parse" && kind != "publication" && kind != "deletion" || state != "" && state != "queued" && state != "running" && state != "completed" && state != "failed" && state != "cancelled" && state != "paused" || documentID != "" && !grantID(documentID) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid job filter")
		return
	}
	var createdAfter, createdBefore time.Time
	for key, target := range map[string]*time.Time{"created_after": &createdAfter, "created_before": &createdBefore} {
		if value := q.Get(key); value != "" {
			parsed, parseErr := time.Parse(time.RFC3339, value)
			if parseErr != nil || len(q[key]) != 1 {
				fail(http.StatusBadRequest, "invalid_argument", "Invalid job time filter")
				return
			}
			*target = parsed
		}
	}
	if !createdAfter.IsZero() && !createdBefore.IsZero() && !createdAfter.Before(createdBefore) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid job time range")
		return
	}
	pageSize := 20
	if value := q.Get("page_size"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100 || len(q["page_size"]) != 1 {
			fail(http.StatusBadRequest, "invalid_argument", "Invalid page size")
			return
		}
		pageSize = n
	}
	cursor := q.Get("cursor")
	if len(cursor) > 10000 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid cursor")
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	scheme, token, ok := strings.Cut(headers[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n,") {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	version, digest, err := h.Service.ProfileIdentity()
	decoded, digestErr := hex.DecodeString(string(digest))
	if err != nil || version == 0 || digestErr != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Job configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	page, err := h.Service.ListJobs(ctx, contracts.JobListInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, Kind: kind, State: state, DocumentID: documentID, CreatedAfter: createdAfter, CreatedBefore: createdBefore, PageSize: pageSize, Cursor: cursor})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Job list deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Jobs unavailable")
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		job := item.Job
		if item.Kind != "parse" && item.Kind != "publication" && item.Kind != "deletion" || job.ID == "" || job.SpaceID == "" || item.ResourceID == "" || !jobStateValidAPI(job.State) || job.Stage == "" || job.RowVersion == 0 || job.Progress < 0 || job.Progress > 1 {
			fail(http.StatusServiceUnavailable, "dependency_unavailable", "Job item unavailable")
			return
		}
		value := map[string]any{"kind": item.Kind, "id": job.ID, "space_id": job.SpaceID, "resource_id": item.ResourceID, "state": job.State, "stage": job.Stage, "attempt": job.Attempt, "row_version": job.RowVersion, "progress": job.Progress}
		if job.Error != nil {
			value["error"] = map[string]any{"code": job.Error.Code, "safe_message": job.Error.SafeMessage, "retryable": job.Error.Retryable, "request_id": job.Error.RequestID}
		}
		items = append(items, value)
	}
	body := map[string]any{"items": items}
	if page.NextCursor != "" {
		if len(page.NextCursor) > 10000 {
			fail(http.StatusServiceUnavailable, "dependency_unavailable", "Job cursor unavailable")
			return
		}
		body["next_cursor"] = page.NextCursor
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func jobStateValidAPI(state string) bool {
	switch state {
	case "queued", "running", "paused", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}
