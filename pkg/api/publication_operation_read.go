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

type PublicationOperationReadApplication = contracts.PublicationOperationReadApplication
type PublicationOperationReadHandler struct {
	Service PublicationOperationReadApplication
}

func (h PublicationOperationReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		fail(http.StatusServiceUnavailable, "capability_missing", "Publication operations not configured")
		return
	}
	operationID := contracts.ID(r.PathValue("id"))
	if !partnerID(operationID) || r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid operation ID")
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
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Publication configuration unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	operation, err := h.Service.ReadPublicationOperation(ctx, contracts.PublicationOperationReadInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, OperationID: operationID})
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Publication operation deadline exceeded")
		return
	}
	if err != nil {
		fail(http.StatusNotFound, "not_found", "Publication operation unavailable")
		return
	}
	if operation.ID == "" || operation.DocumentID == "" || operation.RevisionID == "" || operation.ApprovalID == "" || operation.State == "" {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Publication operation unavailable")
		return
	}
	var generation any
	if operation.GenerationSetID != nil {
		generation = *operation.GenerationSetID
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"operation_id": operation.ID, "document_id": operation.DocumentID, "revision_id": operation.RevisionID, "approval_id": operation.ApprovalID, "expected_epoch": operation.ExpectedEpoch, "state": operation.State, "generation_set_id": generation})
}
