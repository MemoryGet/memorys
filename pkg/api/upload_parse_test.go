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

type parseUploadStore struct{ puts, deletes int }

func (s *parseUploadStore) Put(_ context.Context, _ contracts.BlobWriteRequest, r io.Reader) (contracts.BlobRef, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return contracts.BlobRef{}, err
	}
	if string(b) != "abc" {
		return contracts.BlobRef{}, io.ErrUnexpectedEOF
	}
	s.puts++
	return contracts.BlobRef{ID: "0123456789abcdef0123456789abcdef", SHA256: contracts.Digest(strings.Repeat("a", 64)), Size: 3, MediaType: "text/plain"}, nil
}
func (*parseUploadStore) Open(context.Context, contracts.BlobReadRequest) (io.ReadCloser, error) {
	return nil, io.EOF
}
func (s *parseUploadStore) Delete(context.Context, contracts.BlobReadRequest) error {
	s.deletes++
	return nil
}

type parseUploadCreator struct{ created ingestion.ParseTaskCreate }

func (c *parseUploadCreator) CreateParseTask(_ context.Context, value ingestion.ParseTaskCreate) error {
	c.created = value
	return nil
}

func TestUploadParseHandlerEnqueuesTrustedBinding(t *testing.T) {
	store := &parseUploadStore{}
	creator := &parseUploadCreator{}
	deadline := time.Now().Add(time.Minute)
	h := api.UploadParseHandler{
		Service: ingestion.UploadParseService{Upload: ingestion.UploadService{Store: store}, Creator: creator},
		Prepare: func(*http.Request) (api.UploadParsePreparation, error) {
			return api.UploadParsePreparation{
				Write:     contracts.BlobWriteRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "user", Action: "document.parse", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: deadline}, Size: 3, MediaType: "text/plain", IdempotencyKey: "upload"},
				Principal: contracts.Principal{SubjectID: "user", SubjectType: "user", Issuer: "issuer"}, DocumentID: "doc", DraftID: "draft", CandidateID: "candidate", ExpectedVersion: 1, Format: "txt", Job: contracts.Job{ID: "job", SpaceID: "space", Stage: "document.parse", State: "queued", RowVersion: 1, Attempt: 1},
			}, nil
		},
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("abc"))
	_ = form.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/documents/upload", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	h.Handle(w, r)
	if w.Code != http.StatusOK || store.puts != 1 || creator.created.Command.Input.ID == "" || creator.created.Job.ID != "job" {
		t.Fatalf("status=%d puts=%d body=%s task=%#v", w.Code, store.puts, w.Body.String(), creator.created)
	}
}
