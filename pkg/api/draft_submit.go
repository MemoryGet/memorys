package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type DraftSubmitApplication = contracts.DraftSubmitApplication

type DraftSubmitHandler struct{ Service DraftSubmitApplication }

func (h DraftSubmitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Draft submission not configured")
		return
	}
	draftID := contracts.ID(r.PathValue("draft_id"))
	if !partnerID(draftID) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft ID")
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
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || len(keys[0]) == 0 || len(keys[0]) > 200 || strings.ContainsAny(keys[0], "\r\n") || !utf8.ValidString(keys[0]) {
		fail(http.StatusBadRequest, "invalid_argument", "Idempotency-Key required")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
	if err != nil || len(raw) == 0 || len(raw) > 32<<10 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid submit request")
		return
	}
	if _, err := canonical.JSON(raw); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid submit request")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid submit request")
		return
	}
	for key := range fields {
		if key != "expected_version" {
			fail(http.StatusBadRequest, "invalid_argument", "Invalid submit request")
			return
		}
	}
	var body struct {
		ExpectedVersion uint64 `json:"expected_version"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.ExpectedVersion == 0 || body.ExpectedVersion == math.MaxUint64 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid submit request")
		return
	}
	version, digest, err := h.Service.ProfileIdentity()
	if err != nil || version == 0 || len(digest) != 64 {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Submission configuration unavailable")
		return
	}
	decoded, err := hex.DecodeString(string(digest))
	if err != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Submission configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, err := h.Service.SubmitDraft(ctx, contracts.DraftSubmitInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, DraftID: draftID, ExpectedVersion: body.ExpectedVersion, IdempotencyKey: keys[0]})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Submission deadline exceeded")
		return
	}
	if errors.Is(err, contracts.ErrDraftEditConflict) {
		fail(http.StatusConflict, "conflict", "Draft changed; reload before submitting")
		return
	}
	if errors.Is(err, document.ErrSubmissionRejected) {
		fail(http.StatusUnprocessableEntity, "validation_failed", "Draft is not ready for submission")
		return
	}
	if errors.Is(err, document.ErrSubmissionUnknown) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Submission outcome requires reconciliation")
		return
	}
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Submission unavailable")
		return
	}
	if result.Revision.ID == "" || result.Revision.DocumentID == "" || result.ApprovalRequestID == "" || len(result.Revision.ContentHash) != 64 {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Submission result unavailable")
		return
	}
	revision, err := projectRevision(result.Revision)
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Submission result unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"revision": revision, "approval_request_id": result.ApprovalRequestID})
}
