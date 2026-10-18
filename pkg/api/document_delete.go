package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type DocumentDeletionApplication interface {
	DeleteDocument(context.Context, document.AuthenticatedDeleteInput) (contracts.DeleteDocumentResult, error)
}

type DocumentDeleteHandler struct {
	Service DocumentDeletionApplication
	Profile ProfileIdentitySource
}

func (h DocumentDeleteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var nonce [16]byte
	_, randomErr := rand.Read(nonce[:])
	requestID := hex.EncodeToString(nonce[:])
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-ID", requestID)
	fail := func(status int, code, message string) {
		if status == 401 {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "request_id": requestID, "retryable": status == 503 || status == 504})
	}
	if randomErr != nil {
		fail(500, "internal", "Request unavailable")
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		fail(405, "invalid_argument", "DELETE required")
		return
	}
	if h.Service == nil || h.Profile == nil {
		fail(503, "capability_missing", "Deletion not configured")
		return
	}
	doc := contracts.ID(r.PathValue("id"))
	keys := r.Header.Values("Idempotency-Key")
	var query url.Values
	var e error
	// ParseQuery is used explicitly so malformed escape sequences cannot vanish.
	query, e = url.ParseQuery(r.URL.RawQuery)
	versions := query["expected_document_version"]
	if !partnerID(doc) || len(keys) != 1 || !partnerID(contracts.ID(keys[0])) || e != nil || len(query) != 1 || len(versions) != 1 || versions[0] == "" || len(versions[0]) > 20 {
		fail(400, "invalid_argument", "Invalid deletion request")
		return
	}
	for _, c := range versions[0] {
		if c < '0' || c > '9' {
			fail(400, "invalid_argument", "Invalid document version")
			return
		}
	}
	version, e := strconv.ParseUint(versions[0], 10, 64)
	if e != nil || version == 0 || version == math.MaxUint64 {
		fail(400, "invalid_argument", "Invalid document version")
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 1))
	if e != nil || len(body) != 0 {
		fail(400, "invalid_argument", "Deletion body must be empty")
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	token, ok := strings.CutPrefix(headers[0], "Bearer ")
	if !ok || token == "" {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	for _, b := range []byte(token) {
		if b < 0x21 || b > 0x7e {
			fail(401, "unauthenticated", "Authentication required")
			return
		}
	}
	v, digest, e := h.Profile.ProfileIdentity()
	bytes, de := hex.DecodeString(string(digest))
	if e != nil || v == 0 || de != nil || len(bytes) != 32 || strings.ToLower(string(digest)) != string(digest) {
		fail(503, "dependency_unavailable", "Deletion unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, e := h.Service.DeleteDocument(ctx, document.AuthenticatedDeleteInput{Credential: credential, Request: contracts.RequestContext{RequestID: contracts.ID(requestID), Deadline: deadline, ProfileDigest: digest}, DocumentID: doc, ExpectedDocumentVersion: version, IdempotencyKey: keys[0]})
	if ctx.Err() != nil || errors.Is(e, context.DeadlineExceeded) {
		fail(504, "deadline_exceeded", "Deletion deadline exceeded")
		return
	}
	if errors.Is(e, document.ErrDeletionUnauthenticated) {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	if errors.Is(e, document.ErrDeletionDenied) {
		fail(404, "not_found", "Document unavailable")
		return
	}
	if errors.Is(e, contracts.ErrDocumentDeleteConflict) {
		fail(409, "conflict", "Deletion conflicts with current state")
		return
	}
	if e != nil || result.DocumentID != doc || !partnerID(result.JobID) || result.DocumentVersion != version+1 || result.PublicationEpoch == 0 {
		fail(503, "dependency_unavailable", "Deletion unavailable")
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"document_id": doc, "job_id": result.JobID, "publication_epoch": result.PublicationEpoch})
}
