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

type DraftCompareApplication = contracts.DraftCompareApplication
type DraftCompareHandler struct{ Service DraftCompareApplication }

func (h DraftCompareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Draft comparison not configured")
		return
	}
	draftID := contracts.ID(r.PathValue("draft_id"))
	if !partnerID(draftID) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft ID")
		return
	}
	q := r.URL.Query()
	if len(q) != 1 || len(q["from_version"]) != 1 {
		fail(http.StatusBadRequest, "invalid_argument", "from_version required")
		return
	}
	from, err := strconv.ParseUint(q.Get("from_version"), 10, 64)
	if err != nil || from == 0 {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid from_version")
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
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, err := h.Service.CompareDraft(ctx, contracts.DraftCompareInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, DraftID: draftID, FromVersion: from})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Draft comparison deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusNotFound, "not_found", "Draft version unavailable")
		return
	}
	changes := make([]map[string]any, 0, len(result.Changes))
	for _, change := range result.Changes {
		changes = append(changes, map[string]any{"block_id": change.BlockID, "kind": change.Kind, "before_version": change.BeforeVersion, "after_version": change.AfterVersion})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"draft_id": result.DraftID, "from_version": result.FromVersion, "to_version": result.ToVersion, "changes": changes})
}
