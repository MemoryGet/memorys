package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type DocumentUploadApplication = contracts.DocumentUploadApplication

type DocumentUploadHandler struct {
	Service      DocumentUploadApplication
	MaxBodyBytes int64
}

func (h DocumentUploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := requestNonce(w)
	fail := func(status int, code, message string) {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(http.StatusMethodNotAllowed, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(http.StatusServiceUnavailable, "capability_missing", "Document upload not configured")
		return
	}
	kb := contracts.ID(r.PathValue("kb_id"))
	if !partnerID(kb) {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid knowledge base ID")
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !partnerID(contracts.ID(keys[0])) || len(keys[0]) > 200 {
		fail(http.StatusBadRequest, "invalid_argument", "Idempotency-Key required")
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
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Upload configuration unavailable")
		return
	}
	limit := h.MaxBodyBytes
	if limit <= 0 {
		limit = MaxUploadBody
	}
	if limit > MaxUploadBody {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Upload limit unavailable")
		return
	}
	if r.ContentLength > limit {
		fail(http.StatusRequestEntityTooLarge, "invalid_argument", "Upload too large")
		return
	}
	body := &uploadBody{ReadCloser: http.MaxBytesReader(w, r.Body, limit)}
	r.Body = body
	if err := r.ParseMultipartForm(1 << 20); err != nil || body.exceeded {
		if body.exceeded {
			fail(http.StatusRequestEntityTooLarge, "invalid_argument", "Upload too large")
		} else {
			fail(http.StatusBadRequest, "invalid_argument", "Invalid multipart upload")
		}
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(http.StatusBadRequest, "invalid_argument", "File required")
		return
	}
	defer file.Close()
	if header.Size < 0 || header.Size > limit {
		fail(http.StatusRequestEntityTooLarge, "invalid_argument", "Upload too large")
		return
	}
	tmp, err := os.CreateTemp("", "memorys-upload-")
	if err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Upload unavailable")
		return
	}
	tmpName := tmp.Name()
	defer func() { _ = tmp.Close(); _ = os.Remove(tmpName) }()
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(file, limit+1))
	if err != nil || written != header.Size || written > limit {
		fail(http.StatusRequestEntityTooLarge, "invalid_argument", "Upload too large")
		return
	}
	mediaType := "application/octet-stream"
	if raw := strings.TrimSpace(header.Header.Get("Content-Type")); raw != "" {
		parsed, _, parseErr := mime.ParseMediaType(raw)
		if parseErr != nil || parsed == "" {
			fail(http.StatusBadRequest, "invalid_argument", "Invalid media type")
			return
		}
		mediaType = parsed
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if len(title) > 10000 || strings.ContainsAny(title, "\x00\r\n") {
		fail(http.StatusBadRequest, "invalid_argument", "Invalid title")
		return
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Upload unavailable")
		return
	}
	sum := hasher.Sum(nil)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	result, err := h.Service.UploadDocument(ctx, contracts.DocumentUploadInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, ProfileDigest: digest, Deadline: deadline}, KnowledgeBaseID: kb, Title: title, Size: written, MediaType: mediaType, ExpectedSHA256: contracts.Digest(hex.EncodeToString(sum)), IdempotencyKey: keys[0]}, tmp)
	if ctx.Err() != nil {
		fail(http.StatusGatewayTimeout, "deadline_exceeded", "Upload deadline exceeded")
		return
	}
	if err != nil {
		status, code := http.StatusServiceUnavailable, "dependency_unavailable"
		if errors.Is(err, contracts.ErrDocumentUploadConflict) {
			status, code = http.StatusConflict, "conflict"
		}
		fail(status, code, "Document upload unavailable")
		return
	}
	if result.Document.ID == "" || result.Draft.ID == "" || result.Job.ID == "" || result.Blob.ID == "" {
		fail(http.StatusServiceUnavailable, "dependency_unavailable", "Upload result unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"request_id": requestID, "document": map[string]any{"id": result.Document.ID, "space_id": result.Document.SpaceID, "kb_id": result.Document.KnowledgeBaseID, "title": result.Document.Title, "active_revision_id": nil, "active_generation_set_id": nil, "publication_epoch": result.Document.PublicationEpoch, "row_version": result.Document.RowVersion, "deleted": false}, "draft": map[string]any{"id": result.Draft.ID, "document_id": result.Draft.DocumentID, "base_revision_id": nil, "row_version": result.Draft.RowVersion, "state": result.Draft.State, "blocks": []any{}}, "job": map[string]any{"id": result.Job.ID, "kind": "parse", "space_id": result.Job.SpaceID, "resource_id": result.Document.ID, "state": result.Job.State, "stage": result.Job.Stage, "attempt": result.Job.Attempt, "row_version": result.Job.RowVersion, "progress": result.Job.Progress}, "blob": map[string]any{"id": result.Blob.ID, "sha256": result.Blob.SHA256, "size": result.Blob.Size, "media_type": result.Blob.MediaType}})
}
