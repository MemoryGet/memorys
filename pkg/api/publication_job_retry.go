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

type PublicationJobRetryApplication interface {
	RetryJob(context.Context, document.JobRetryInput) (contracts.PublicationTaskRetryResult, error)
}

// PublicationJobRetryHandler returns a new job because retry creates a new
// publication intent; the failed job remains an immutable historical result.
type PublicationJobRetryHandler struct {
	Service PublicationJobRetryApplication
}

func (h PublicationJobRetryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		http.Error(w, "request unavailable", http.StatusInternalServerError)
		return
	}
	requestID := hex.EncodeToString(nonce[:])
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-ID", requestID)
	fail := func(status int, code, message string) {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "request_id": requestID, "retryable": status == 503 || status == 504})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Publication retry not configured")
		return
	}
	jobID := contracts.ID(r.PathValue("id"))
	keys := r.Header.Values("Idempotency-Key")
	if !partnerID(jobID) || len(keys) != 1 || !partnerID(contracts.ID(keys[0])) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid publication retry request")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil || len(body) == 16385 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid publication retry body")
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
	if !ok || token == "" || strings.ContainsAny(token, " \t\r\n,") {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, err := h.Service.RetryJob(ctx, document.JobRetryInput{Credential: credential, Request: contracts.RequestContext{RequestID: contracts.ID(requestID), Deadline: deadline}, JobID: jobID, ExpectedVersion: command.ExpectedJobVersion, IdempotencyKey: keys[0]})
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Publication retry deadline exceeded")
		return
	}
	if errors.Is(err, document.ErrPublicationRetryUnauthenticated) {
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	if errors.Is(err, document.ErrPublicationRetryDenied) {
		fail(http.StatusConflict, "conflict", "Publication job cannot be retried")
		return
	}
	if err != nil || result.PreviousJobID != jobID || result.Job.Job.ID == "" {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Publication retry unavailable")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"previous_job_id": result.PreviousJobID, "job_id": result.Job.Job.ID, "state": result.Job.Job.State, "row_version": result.Job.Job.RowVersion})
}
