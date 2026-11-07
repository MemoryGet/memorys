package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type ReviewDecisionInput = contracts.ReviewDecisionInput

type ReviewDecisionApplication = contracts.ReviewDecisionApplication

// ReviewDecisionHandler exposes the approval-decision management boundary.
// The application owns document lookup, reviewer authorization, CAS and the
// durable review transaction; the transport accepts no client-supplied actor
// or document binding.
type ReviewDecisionHandler struct{ Service ReviewDecisionApplication }

func (h ReviewDecisionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Review not configured")
		return
	}
	revisionID := contracts.ID(r.PathValue("revision_id"))
	if !partnerID(revisionID) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid revision ID")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(http.StatusUnsupportedMediaType, "invalid_argument", "JSON required")
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
	idempotency := r.Header.Values("Idempotency-Key")
	if len(idempotency) != 1 || len(idempotency[0]) == 0 || len(idempotency[0]) > 200 || strings.ContainsAny(idempotency[0], "\r\n") || !utf8.ValidString(idempotency[0]) {
		fail(http.StatusBadRequest, "invalid_argument", "Idempotency-Key required")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
	if err != nil || len(raw) > 32<<10 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid review request")
		return
	}
	if _, err := canonical.JSON(raw); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid review request")
		return
	}
	var input struct {
		Decision                string `json:"decision"`
		Reason                  string `json:"reason"`
		ExpectedApprovalVersion uint64 `json:"expected_approval_version"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || (input.Decision != "approve" && input.Decision != "reject" && input.Decision != "revoke") || !utf8.ValidString(input.Reason) || len(input.Reason) > 10000 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid review decision")
		return
	}
	version, digest, err := h.Service.ProfileIdentity()
	if err != nil || version == 0 || len(digest) != 64 {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Review configuration unavailable")
		return
	}
	if decoded, err := hex.DecodeString(string(digest)); err != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Review configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	approval, err := h.Service.DecideReview(ctx, contracts.ReviewDecisionInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, RevisionID: revisionID, Decision: input.Decision, Reason: input.Reason, ExpectedApprovalVersion: input.ExpectedApprovalVersion, IdempotencyKey: idempotency[0]})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Review deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Review unavailable")
		return
	}
	if approval.ID == "" || approval.RevisionID != revisionID || approval.Decision != input.Decision || approval.ActorID == "" || approval.ContentHash == "" || approval.RowVersion == 0 || approval.DecidedAt.IsZero() || approval.PolicyVersion == 0 {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Review result unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": approval.ID, "revision_id": approval.RevisionID, "decision": approval.Decision, "actor_id": approval.ActorID, "reason": approval.Reason, "content_hash": approval.ContentHash, "policy_version": approval.PolicyVersion, "row_version": approval.RowVersion, "decided_at": approval.DecidedAt.UTC().Format(time.RFC3339Nano)})
}
