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

type GrantListApplication = contracts.GrantListApplication

type GrantListHandler struct{ Service GrantListApplication }

func (h GrantListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Grant list not configured")
		return
	}
	kb := contracts.ID(r.PathValue("kb_id"))
	if !grantID(kb) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid knowledge base ID")
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
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Grant configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	page, err := h.Service.ListGrants(ctx, contracts.GrantListInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, KnowledgeBaseID: kb, PageSize: pageSize, Cursor: cursor})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Grant list deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Grants unavailable")
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		if item.KnowledgeBaseID != kb || !grantID(item.SubjectID) || !grantRole(item.Role) || item.Role == "none" || item.PolicyVersion == 0 {
			fail(http.StatusServiceUnavailable, "dependency_unavailable", "Grant item unavailable")
			return
		}
		items = append(items, map[string]any{"kb_id": item.KnowledgeBaseID, "subject_id": item.SubjectID, "role": item.Role, "policy_version": item.PolicyVersion})
	}
	body := map[string]any{"items": items}
	if page.NextCursor != "" {
		if len(page.NextCursor) > 10000 {
			fail(http.StatusServiceUnavailable, "dependency_unavailable", "Grant cursor unavailable")
			return
		}
		body["next_cursor"] = page.NextCursor
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
