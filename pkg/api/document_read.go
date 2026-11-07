package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type DocumentReadApplication = contracts.DocumentReadApplication

type DocumentReadHandler struct{ Service DocumentReadApplication }

func (h DocumentReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Document read not configured")
		return
	}
	id := contracts.ID(r.PathValue("id"))
	if !grantID(id) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid document request")
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
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Document configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	document, err := h.Service.ReadDocument(ctx, contracts.DocumentReadInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, DocumentID: id})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Document deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Document unavailable")
		return
	}
	var revision, generation any
	if document.ActiveRevisionID != nil {
		revision = *document.ActiveRevisionID
	}
	if document.ActiveGenerationSetID != nil {
		generation = *document.ActiveGenerationSetID
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": document.ID, "space_id": document.SpaceID, "kb_id": document.KnowledgeBaseID, "title": document.Title, "active_revision_id": revision, "active_generation_set_id": generation, "publication_epoch": document.PublicationEpoch, "row_version": document.RowVersion, "deleted": document.DeletedAt != nil})
}
