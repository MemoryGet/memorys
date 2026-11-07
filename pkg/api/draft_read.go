package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type DraftReadApplication = contracts.DraftReadApplication

type DraftReadHandler struct {
	Service DraftReadApplication
}

func (h DraftReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Draft reads not configured")
		return
	}
	draftID := contracts.ID(r.PathValue("draft_id"))
	if !partnerID(draftID) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid draft request")
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
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft configuration unavailable")
		return
	}
	decoded, err := hex.DecodeString(string(digest))
	if err != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	draft, err := h.Service.ReadDraft(ctx, contracts.DraftReadInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, DraftID: draftID})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Draft read deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusNotFound, "not_found", "Draft unavailable")
		return
	}
	body, err := projectDraft(draft)
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Draft unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// projectDraft converts the in-process content union to the frozen management
// representation while retaining mutable block versions for compare-and-swap
// edits. BuildFrozenContent supplies the same validation and canonical shape
// used by submission, so reads cannot expose malformed content.
func projectDraft(draft contracts.Draft) (map[string]any, error) {
	if draft.ID == "" || draft.DocumentID == "" || draft.RowVersion == 0 || (draft.State != "draft" && draft.State != "submitted") || len(draft.Blocks) == 0 {
		if draft.ID == "" || draft.DocumentID == "" || draft.RowVersion == 0 || (draft.State != "draft" && draft.State != "submitted") {
			return nil, ErrReadProjection
		}
		var base any
		if draft.BaseRevisionID != nil {
			base = *draft.BaseRevisionID
		}
		return map[string]any{"id": draft.ID, "document_id": draft.DocumentID, "base_revision_id": base, "row_version": draft.RowVersion, "state": draft.State, "blocks": []any{}}, nil
	}
	canonicalDraft := draft
	canonicalDraft.State = "draft"
	frozen, err := document.BuildFrozenContent(canonicalDraft)
	if err != nil {
		return nil, ErrReadProjection
	}
	var envelope struct {
		Blocks []map[string]json.RawMessage `json:"blocks"`
	}
	if json.Unmarshal(frozen.Payload, &envelope) != nil || len(envelope.Blocks) != len(draft.Blocks) {
		return nil, ErrReadProjection
	}
	blocks := make([]map[string]any, 0, len(draft.Blocks))
	for i, block := range draft.Blocks {
		if block.ID == "" || block.DraftID != draft.ID || block.RowVersion == 0 || block.Order != i {
			return nil, ErrReadProjection
		}
		var kind, text string
		if json.Unmarshal(envelope.Blocks[i]["kind"], &kind) != nil || json.Unmarshal(envelope.Blocks[i]["text"], &text) != nil {
			return nil, ErrReadProjection
		}
		item := map[string]any{"id": block.ID, "kind": kind, "text": text, "draft_id": draft.ID, "row_version": block.RowVersion, "source_anchor": json.RawMessage(envelope.Blocks[i]["source_anchor"])}
		if raw, ok := envelope.Blocks[i]["structured_content"]; ok {
			item["structured_content"] = json.RawMessage(raw)
		}
		blocks = append(blocks, item)
	}
	var base any
	if draft.BaseRevisionID != nil {
		base = *draft.BaseRevisionID
	}
	return map[string]any{"id": draft.ID, "document_id": draft.DocumentID, "base_revision_id": base, "row_version": draft.RowVersion, "state": draft.State, "blocks": blocks}, nil
}
