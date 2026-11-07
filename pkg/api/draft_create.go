package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type DraftCreateApplication = contracts.DraftCreateApplication
type DraftCreateHandler struct{ Service DraftCreateApplication }

func (h DraftCreateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Draft creation not configured")
		return
	}
	documentID := contracts.ID(r.PathValue("id"))
	if !partnerID(documentID) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid document ID")
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
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft request")
		return
	}
	if _, err := canonical.JSON(raw); err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft request")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft request")
		return
	}
	for key := range fields {
		if key != "base_revision_id" && key != "expected_document_version" {
			fail(http.StatusBadRequest, "invalid_argument", "Invalid draft request")
			return
		}
	}
	var body struct {
		BaseRevisionID          *contracts.ID `json:"base_revision_id"`
		ExpectedDocumentVersion uint64        `json:"expected_document_version"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.ExpectedDocumentVersion == 0 || body.ExpectedDocumentVersion == math.MaxUint64 || body.BaseRevisionID != nil && !partnerID(*body.BaseRevisionID) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft request")
		return
	}
	version, digest, err := h.Service.ProfileIdentity()
	decoded, digestErr := hex.DecodeString(string(digest))
	if err != nil || version == 0 || digestErr != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	draft, err := h.Service.CreateDraft(ctx, contracts.DraftCreateInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, DocumentID: documentID, BaseRevisionID: body.BaseRevisionID, ExpectedDocumentVersion: body.ExpectedDocumentVersion, IdempotencyKey: keys[0]})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Draft creation deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusConflict, "conflict", "Draft creation preconditions changed")
		return
	}
	bodyOut, err := projectDraft(draft)
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft result unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(bodyOut)
}
