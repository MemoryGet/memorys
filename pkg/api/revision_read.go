package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type RevisionReadApplication = contracts.RevisionReadApplication

type RevisionReadHandler struct{ Service RevisionReadApplication }

func (h RevisionReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Revision reads not configured")
		return
	}
	revisionID := contracts.ID(r.PathValue("revision_id"))
	if !partnerID(revisionID) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid revision request")
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
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Revision configuration unavailable")
		return
	}
	decoded, err := hex.DecodeString(string(digest))
	if err != nil || len(decoded) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Revision configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	revision, err := h.Service.ReadRevision(ctx, contracts.RevisionReadInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, RevisionID: revisionID})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Revision read deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusNotFound, "not_found", "Revision unavailable")
		return
	}
	body, err := projectRevision(revision)
	if errors.Is(err, ErrReadProjection) || err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Revision unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func projectRevision(revision contracts.Revision) (map[string]any, error) {
	if revision.ID == "" || revision.DocumentID == "" || revision.HashFormat != "akos.freeze/1" || len(revision.ContentHash) != 64 || len(revision.BlocksHash) != 64 || revision.CreatedBy == "" || revision.CreatedAt.IsZero() || len(revision.Blocks) == 0 || len(revision.Blocks) > 10000 {
		return nil, ErrReadProjection
	}
	draft := contracts.Draft{ID: contracts.ID("revision-projection-" + string(revision.ID)), DocumentID: revision.DocumentID, RowVersion: 1, State: "draft", Blobs: append([]contracts.BlobRef(nil), revision.Blobs...)}
	for index, block := range revision.Blocks {
		if block.ID == "" || block.RevisionID != revision.ID || block.Order != index {
			return nil, ErrReadProjection
		}
		draft.Blocks = append(draft.Blocks, contracts.DraftBlock{ID: block.ID, DraftID: draft.ID, RowVersion: 1, Order: block.Order, Content: block.Content})
	}
	frozen, err := document.BuildFrozenContent(draft)
	if err != nil || frozen.ContentHash != revision.ContentHash || frozen.BlocksHash != revision.BlocksHash {
		return nil, ErrReadProjection
	}
	var envelope struct {
		Blocks []map[string]json.RawMessage `json:"blocks"`
	}
	if json.Unmarshal(frozen.Payload, &envelope) != nil || len(envelope.Blocks) != len(revision.Blocks) {
		return nil, ErrReadProjection
	}
	blocks := make([]any, 0, len(revision.Blocks))
	for i, source := range revision.Blocks {
		var kind, text string
		if json.Unmarshal(envelope.Blocks[i]["kind"], &kind) != nil || json.Unmarshal(envelope.Blocks[i]["text"], &text) != nil {
			return nil, ErrReadProjection
		}
		item := map[string]any{"id": source.ID, "revision_id": revision.ID, "kind": kind, "text": text, "source_anchor": json.RawMessage(envelope.Blocks[i]["source_anchor"])}
		if raw, ok := envelope.Blocks[i]["structured_content"]; ok {
			item["structured_content"] = json.RawMessage(raw)
		}
		blocks = append(blocks, item)
	}
	blobs := make([]any, 0, len(revision.Blobs))
	for _, blob := range revision.Blobs {
		if blob.ID == "" || len(blob.SHA256) != 64 || blob.Size < 0 || blob.MediaType == "" {
			return nil, ErrReadProjection
		}
		blobs = append(blobs, map[string]any{"id": blob.ID, "sha256": blob.SHA256, "size": blob.Size, "media_type": blob.MediaType})
	}
	var parent any
	if revision.ParentRevisionID != nil {
		parent = *revision.ParentRevisionID
	}
	return map[string]any{"id": revision.ID, "document_id": revision.DocumentID, "parent_revision_id": parent, "content_hash": revision.ContentHash, "blocks_hash": revision.BlocksHash, "hash_format": revision.HashFormat, "blobs": blobs, "created_by": revision.CreatedBy, "created_at": revision.CreatedAt.UTC().Format(time.RFC3339Nano), "blocks": blocks}, nil
}
