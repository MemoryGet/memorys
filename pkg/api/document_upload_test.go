package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type documentUploadFixture struct{}

func (documentUploadFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}
func (documentUploadFixture) UploadDocument(_ context.Context, in contracts.DocumentUploadInput, _ io.Reader) (contracts.DocumentUploadResult, error) {
	return contracts.DocumentUploadResult{Document: contracts.Document{ID: "doc1", SpaceID: "space1", KnowledgeBaseID: in.KnowledgeBaseID, Title: in.Title, RowVersion: 1}, Draft: contracts.Draft{ID: "draft1", DocumentID: "doc1", RowVersion: 1, State: "draft"}, Job: contracts.Job{ID: "job1", SpaceID: "space1", Stage: "document.parse", State: "queued", Attempt: 1, RowVersion: 1}, Blob: contracts.BlobRef{ID: "blob1", SHA256: in.ExpectedSHA256, Size: in.Size, MediaType: in.MediaType}}, nil
}

func TestDocumentUploadHandlerAcceptsMultipart(t *testing.T) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("title", "说明")
	part, err := form.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("hello"))
	_ = form.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/knowledge-bases/kb1/documents", &body)
	r.SetPathValue("kb_id", "kb1")
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "upload-key")
	w := httptest.NewRecorder()
	(DocumentUploadHandler{Service: documentUploadFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"draft"`) || !strings.Contains(w.Body.String(), `"job"`) {
		t.Fatalf("upload response: %d %s", w.Code, w.Body.String())
	}
}

func TestDocumentUploadHandlerRejectsMissingIdempotency(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/knowledge-bases/kb1/documents", strings.NewReader(""))
	r.SetPathValue("kb_id", "kb1")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(DocumentUploadHandler{Service: documentUploadFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency accepted: %d %s", w.Code, w.Body.String())
	}
}
