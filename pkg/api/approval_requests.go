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

type ApprovalRequestListApplication = contracts.ApprovalRequestListApplication

type ApprovalRequestListHandler struct {
	Service ApprovalRequestListApplication
}

func (h ApprovalRequestListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Approval requests not configured")
		return
	}
	kb := contracts.ID(r.PathValue("kb_id"))
	if !partnerID(kb) {
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
	if err != nil || version == 0 || len(digest) != 64 {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Approval configuration unavailable")
		return
	}
	if decoded, err := hex.DecodeString(string(digest)); err != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Approval configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	page, err := h.Service.ListApprovalRequests(ctx, contracts.ApprovalRequestListInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, KnowledgeBaseID: kb, PageSize: pageSize, Cursor: cursor})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Approval request deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Approval requests unavailable")
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		if item.ID == "" || item.RevisionID == "" || item.DocumentID == "" || item.SubmittedBy == "" || item.SubmittedAt.IsZero() || item.RowVersion > 1<<63-1 || item.PolicyVersion == 0 || len(item.ContentHash) != 64 {
			fail(http.StatusServiceUnavailable, "dependency_unavailable", "Approval request unavailable")
			return
		}
		items = append(items, map[string]any{"id": item.ID, "revision_id": item.RevisionID, "document_id": item.DocumentID, "submitted_by": item.SubmittedBy, "submitted_at": item.SubmittedAt.UTC().Format(time.RFC3339Nano), "state": item.State, "row_version": item.RowVersion, "content_hash": item.ContentHash, "policy_version": item.PolicyVersion})
	}
	result := map[string]any{"items": items}
	if page.NextCursor != "" {
		if len(page.NextCursor) > 10000 {
			fail(http.StatusServiceUnavailable, "dependency_unavailable", "Approval cursor unavailable")
			return
		}
		result["next_cursor"] = page.NextCursor
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
