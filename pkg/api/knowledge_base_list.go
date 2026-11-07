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

type KnowledgeBaseListApplication = contracts.KnowledgeBaseListApplication
type KnowledgeBaseListHandler struct{ Service KnowledgeBaseListApplication }

func (h KnowledgeBaseListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Knowledge base listing not configured")
		return
	}
	space := contracts.ID(r.PathValue("space_id"))
	if !partnerID(space) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid space ID")
		return
	}
	q := r.URL.Query()
	for key := range q {
		if key != "cursor" && key != "page_size" {
			fail(http.StatusBadRequest, "invalid_argument", "Unknown query parameter")
			return
		}
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
	if len(q["cursor"]) > 1 || len(cursor) > 10000 {
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
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Knowledge base configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	page, err := h.Service.ListKnowledgeBases(ctx, contracts.KnowledgeBaseListInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, SpaceID: space, PageSize: pageSize, Cursor: cursor})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Knowledge base listing deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Knowledge bases unavailable")
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		if item.ID == "" || item.SpaceID != space || item.RowVersion == 0 || item.PolicyVersion == 0 {
			fail(http.StatusServiceUnavailable, "dependency_unavailable", "Knowledge base unavailable")
			return
		}
		var backend any
		if item.ActiveBackendEpochID != nil {
			backend = *item.ActiveBackendEpochID
		}
		items = append(items, map[string]any{"id": item.ID, "space_id": item.SpaceID, "name": item.Name, "active_backend_epoch_id": backend, "row_version": item.RowVersion, "policy_version": item.PolicyVersion})
	}
	body := map[string]any{"items": items}
	if page.NextCursor != "" {
		body["next_cursor"] = page.NextCursor
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
