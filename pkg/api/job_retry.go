package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type JobRetryApplication interface {
	RetryJob(context.Context, document.JobRetryInput) (contracts.ResourceJob, error)
}

type JobRetryHandler struct{ Service JobRetryApplication }

func (h JobRetryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		http.Error(w, "request unavailable", http.StatusInternalServerError)
		return
	}
	id := hex.EncodeToString(nonce[:])
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-ID", id)
	fail := func(status int, code, msg string) {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": msg, "request_id": id, "retryable": status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Job retry not configured")
		return
	}
	jobID := contracts.ID(r.PathValue("id"))
	keys := r.Header.Values("Idempotency-Key")
	if !partnerID(jobID) || len(keys) != 1 || !partnerID(contracts.ID(keys[0])) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid job retry request")
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 16385))
	if e != nil || len(body) == 16385 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid job retry body")
		return
	}
	var command struct {
		ExpectedJobVersion uint64 `json:"expected_job_version"`
	}
	if len(body) == 0 || json.Unmarshal(body, &command) != nil || command.ExpectedJobVersion == 0 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid expected job version")
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	token, ok := strings.CutPrefix(headers[0], "Bearer ")
	if !ok || token == "" {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	for _, b := range []byte(token) {
		if b < 0x21 || b > 0x7e {
			fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, e := h.Service.RetryJob(ctx, document.JobRetryInput{Credential: credential, Request: contracts.RequestContext{RequestID: contracts.ID(id), Deadline: deadline}, JobID: jobID, ExpectedVersion: command.ExpectedJobVersion, IdempotencyKey: keys[0]})
	if ctx.Err() != nil || errors.Is(e, context.DeadlineExceeded) {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Job retry deadline exceeded")
		return
	}
	if errors.Is(e, document.ErrJobRetryUnauthenticated) {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	if errors.Is(e, document.ErrJobRetryDenied) {
		fail(http.StatusConflict, "conflict", "Job cannot be retried")
		return
	}
	if e != nil || result.Job.ID != jobID {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Job retry unavailable")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"job_id": jobID, "state": "accepted"})
}
