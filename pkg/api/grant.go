package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type GrantApplication = contracts.GrantApplication

type GrantHandler struct{ Service GrantApplication }

func (h GrantHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := grantRequestID(w)
	fail := func(status int, code, message string) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "PUT required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Grant management not configured")
		return
	}
	kb, subject := contracts.ID(r.PathValue("kb_id")), contracts.ID(r.PathValue("subject_id"))
	if !grantID(kb) || !grantID(subject) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid grant target")
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !grantID(contracts.ID(keys[0])) || len(keys[0]) > 200 {
		fail(http.StatusBadRequest, "invalid_argument", "Idempotency-Key required")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(http.StatusUnsupportedMediaType, "invalid_argument", "JSON required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(raw) == 0 || len(raw) > 64<<10 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid grant request")
		return
	}
	if _, err := canonical.JSON(raw); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid grant request")
		return
	}
	var input struct {
		Role                  string `json:"role"`
		ExpectedPolicyVersion uint64 `json:"expected_policy_version"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || !grantRole(input.Role) || input.ExpectedPolicyVersion == 0 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid grant request")
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
	if err != nil || version == 0 || len(digest) != 64 {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Grant configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, err := h.Service.SetGrant(ctx, contracts.SetGrantInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, KnowledgeBaseID: kb, SubjectID: subject, Role: input.Role, ExpectedPolicyVersion: input.ExpectedPolicyVersion, IdempotencyKey: keys[0]})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Grant deadline exceeded")
		return
	}
	switch {
	case errors.Is(err, contracts.ErrGrantUnauthenticated):
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
	case errors.Is(err, contracts.ErrGrantDenied):
		fail(http.StatusForbidden, "forbidden", "Grant management denied")
	case errors.Is(err, contracts.ErrGrantConflict), errors.Is(err, contracts.ErrGrantIdempotency):
		fail(http.StatusConflict, "conflict", "Grant policy changed or idempotency key conflicts")
	case err != nil:
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Grant management unavailable")
	default:
		if result.KnowledgeBaseID != kb || result.SubjectID != subject || !grantRole(result.Role) || result.PolicyVersion == 0 {
			fail(http.StatusServiceUnavailable, "dependency_unavailable", "Grant result unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"kb_id": result.KnowledgeBaseID, "subject_id": result.SubjectID, "role": result.Role, "policy_version": result.PolicyVersion})
	}
}

func grantRequestID(w http.ResponseWriter) contracts.ID {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "request-unavailable"
	}
	id := contracts.ID(hex.EncodeToString(nonce[:]))
	w.Header().Set("X-Request-ID", string(id))
	return id
}

func grantID(value contracts.ID) bool {
	return value != "" && len(value) <= 200 && !strings.ContainsAny(string(value), "\x00\r\n")
}

func grantRole(value string) bool {
	switch value {
	case "Admin", "Contributor", "Viewer", "none":
		return true
	default:
		return false
	}
}
