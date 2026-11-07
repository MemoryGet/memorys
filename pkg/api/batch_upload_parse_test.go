package api_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
)

type batchUploadStore struct{ puts int }

func (s *batchUploadStore) Put(_ context.Context, _ contracts.BlobWriteRequest, r io.Reader) (contracts.BlobRef, error) {
	if _, err := io.ReadAll(r); err != nil {
		return contracts.BlobRef{}, err
	}
	s.puts++
	return contracts.BlobRef{ID: "0123456789abcdef0123456789abcdef", SHA256: contracts.Digest(strings.Repeat("a", 64)), Size: 3, MediaType: "text/plain"}, nil
}
func (*batchUploadStore) Open(context.Context, contracts.BlobReadRequest) (io.ReadCloser, error) {
	return nil, io.EOF
}
func (*batchUploadStore) Delete(context.Context, contracts.BlobReadRequest) error { return nil }

type batchUploadCreator struct{ jobs int }

func (c *batchUploadCreator) CreateParseTask(_ context.Context, _ ingestion.ParseTaskCreate) error {
	c.jobs++
	return nil
}

func TestBatchUploadParseHandlerReturnsPerFileResults(t *testing.T) {
	store := &batchUploadStore{}
	creator := &batchUploadCreator{}
	deadline := time.Now().Add(time.Minute)
	h := api.BatchUploadParseHandler{
		Service:  ingestion.UploadParseService{Upload: ingestion.UploadService{Store: store}, Creator: creator},
		MaxFiles: 2,
		Prepare: func(_ *http.Request, index int, header *multipart.FileHeader) (api.UploadParsePreparation, error) {
			if index == 1 {
				return api.UploadParsePreparation{Write: contracts.BlobWriteRequest{Request: contracts.RequestContext{Deadline: deadline}, Size: 4}}, nil
			}
			return api.UploadParsePreparation{
				Write:     contracts.BlobWriteRequest{Request: contracts.RequestContext{RequestID: contracts.ID(header.Filename), Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "user", Action: "document.parse", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: deadline}, Size: 3, MediaType: "text/plain"},
				Principal: contracts.Principal{SubjectID: "user", SubjectType: "user", Issuer: "issuer"}, DocumentID: "doc", DraftID: "draft", CandidateID: "candidate", ExpectedVersion: 1, Format: "txt", Job: contracts.Job{ID: "job", State: "queued"},
			}, nil
		},
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, name := range []string{"one.txt", "two.txt"} {
		file, err := form.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.Write([]byte("abc"))
	}
	_ = form.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/documents/batch-upload", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	h.Handle(w, r)
	if w.Code != http.StatusOK || store.puts != 1 || creator.jobs != 1 || !strings.Contains(w.Body.String(), `"count":2`) || !strings.Contains(w.Body.String(), `"file size mismatch"`) {
		t.Fatalf("status=%d puts=%d jobs=%d body=%s", w.Code, store.puts, creator.jobs, w.Body.String())
	}
}
