package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type JobStatusApplication interface {
	GetJob(context.Context, document.JobStatusInput) (contracts.ResourceJob, error)
}
type JobStatusHandler struct{ Service JobStatusApplication }

func (h JobStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var nonce [16]byte
	_, randomErr := rand.Read(nonce[:])
	requestID := hex.EncodeToString(nonce[:])
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-ID", requestID)
	fail := func(status int, code, message string) {
		if status == 401 {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "request_id": requestID, "retryable": status == 503 || status == 504})
	}
	if randomErr != nil {
		fail(500, "internal", "Request unavailable")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(405, "invalid_argument", "GET required")
		return
	}
	if h.Service == nil {
		fail(503, "capability_missing", "Job status not configured")
		return
	}
	id := contracts.ID(r.PathValue("id"))
	if !partnerID(id) || r.URL.RawQuery != "" {
		fail(400, "invalid_argument", "Invalid job request")
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 1))
	if e != nil || len(body) != 0 {
		fail(400, "invalid_argument", "Job request body must be empty")
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	token, ok := strings.CutPrefix(headers[0], "Bearer ")
	if !ok || token == "" {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	for _, b := range []byte(token) {
		if b < 0x21 || b > 0x7e {
			fail(401, "unauthenticated", "Authentication required")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	record, e := h.Service.GetJob(ctx, document.JobStatusInput{Credential: credential, Request: contracts.RequestContext{RequestID: contracts.ID(requestID), Deadline: deadline}, JobID: id})
	if ctx.Err() != nil || errors.Is(e, context.DeadlineExceeded) {
		fail(504, "deadline_exceeded", "Job deadline exceeded")
		return
	}
	if errors.Is(e, document.ErrJobUnauthenticated) {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	if errors.Is(e, contracts.ErrJobReadDenied) {
		fail(404, "not_found", "Job unavailable")
		return
	}
	j := record.Job
	if e != nil || j.ID != id || !partnerID(record.ResourceID) || !partnerID(j.SpaceID) || j.RowVersion == 0 || j.Stage == "" || !utf8.ValidString(j.Stage) || utf8.RuneCountInString(j.Stage) > 10000 || math.IsNaN(j.Progress) || math.IsInf(j.Progress, 0) || j.Progress < 0 || j.Progress > 1 {
		fail(503, "dependency_unavailable", "Job unavailable")
		return
	}
	switch j.State {
	case "queued", "running", "completed", "failed", "cancelled":
	default:
		fail(503, "dependency_unavailable", "Job unavailable")
		return
	}
	result := map[string]any{"id": j.ID, "space_id": j.SpaceID, "resource_id": record.ResourceID, "state": j.State, "stage": j.Stage, "attempt": j.Attempt, "row_version": j.RowVersion, "progress": j.Progress}
	if j.Error != nil {
		e := j.Error
		allowed := false
		switch e.Code {
		case "invalid_argument", "validation_failed", "unsupported_format", "unauthenticated", "forbidden", "not_found", "conflict", "publication_changed", "cursor_expired", "capability_missing", "contract_incompatible", "rate_limited", "deadline_exceeded", "dependency_unavailable", "internal":
			allowed = true
		}
		if !allowed || !partnerID(e.RequestID) || !utf8.ValidString(e.SafeMessage) || utf8.RuneCountInString(e.SafeMessage) > 10000 {
			fail(503, "dependency_unavailable", "Job unavailable")
			return
		}
		result["error"] = map[string]any{"code": e.Code, "safe_message": e.SafeMessage, "retryable": e.Retryable, "request_id": e.RequestID}
	}
	_ = json.NewEncoder(w).Encode(result)
}
