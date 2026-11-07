package api

import (
	"context"
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

type KnowledgeBaseCreateApplication = contracts.KnowledgeBaseCreateApplication
type KnowledgeBaseCreateHandler struct {
	Service KnowledgeBaseCreateApplication
}

func (h KnowledgeBaseCreateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := requestNonce(w)
	fail := func(status int, code, message string) {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Knowledge base creation not configured")
		return
	}
	space := contracts.ID(r.PathValue("space_id"))
	if !partnerID(space) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid space ID")
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !partnerID(contracts.ID(keys[0])) || len(keys[0]) > 200 {
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
		fail(http.StatusBadRequest, "invalid_argument", "Invalid knowledge base request")
		return
	}
	if _, err = canonical.JSON(raw); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid knowledge base request")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || strings.TrimSpace(body.Name) == "" || len(body.Name) > 10000 || strings.ContainsAny(body.Name, "\x00\r\n") {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid knowledge base name")
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
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Knowledge base configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	kb, err := h.Service.CreateKnowledgeBase(ctx, contracts.KnowledgeBaseCreateInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, SpaceID: space, Name: strings.TrimSpace(body.Name), IdempotencyKey: keys[0]})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Knowledge base creation deadline exceeded")
		return
	}
	switch {
	case errors.Is(err, contracts.ErrKnowledgeBaseCreateUnauthenticated):
		fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
	case errors.Is(err, contracts.ErrKnowledgeBaseCreateDenied):
		fail(http.StatusForbidden, "forbidden", "Knowledge base creation denied")
	case errors.Is(err, contracts.ErrKnowledgeBaseCreateConflict):
		fail(http.StatusConflict, "conflict", "Knowledge base idempotency conflict")
	case err != nil:
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Knowledge base creation unavailable")
	default:
		if kb.ID == "" || kb.SpaceID != space || kb.Name == "" || kb.RowVersion == 0 || kb.PolicyVersion == 0 {
			fail(http.StatusServiceUnavailable, "dependency_unavailable", "Knowledge base result unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": kb.ID, "space_id": kb.SpaceID, "name": kb.Name, "active_backend_epoch_id": kb.ActiveBackendEpochID, "row_version": kb.RowVersion, "policy_version": kb.PolicyVersion})
	}
}
