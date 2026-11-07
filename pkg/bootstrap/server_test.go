package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"github.com/MemoryGet/memorys/pkg/knowledge"
	"time"
)

type fakeRead struct{}

func (fakeRead) ReadCurrent(context.Context, knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error) {
	return contracts.FrozenContentRecord{}, nil
}

type fakeDelete struct{}

func (fakeDelete) DeleteDocument(context.Context, document.AuthenticatedDeleteInput) (contracts.DeleteDocumentResult, error) {
	return contracts.DeleteDocumentResult{}, nil
}

type configuredUploadStore struct{ writes int }

func (s *configuredUploadStore) Put(context.Context, contracts.BlobWriteRequest, io.Reader) (contracts.BlobRef, error) {
	s.writes++
	return contracts.BlobRef{ID: "0123456789abcdef0123456789abcdef", SHA256: contracts.Digest(strings.Repeat("a", 64)), Size: 3, MediaType: "text/plain"}, nil
}
func (*configuredUploadStore) Open(context.Context, contracts.BlobReadRequest) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}
func (*configuredUploadStore) Delete(context.Context, contracts.BlobReadRequest) error { return nil }

type fakeWorker struct {
	started bool
	stopped bool
}

func (w *fakeWorker) Start(context.Context) error { w.started = true; return nil }
func (w *fakeWorker) Stop() error                 { w.stopped = true; return nil }
func (w *fakeWorker) Ready() error {
	if !w.started || w.stopped {
		return errors.New("worker not ready")
	}
	return nil
}

func TestValidateRejectsPartialReadAndDeletionWiring(t *testing.T) {
	if _, err := New(Config{Read: fakeRead{}}); err == nil {
		t.Fatal("read without search accepted")
	}
	if _, err := New(Config{Deletion: fakeDelete{}}); err == nil {
		t.Fatal("deletion without profile accepted")
	}
}

func TestNewKeepsMissingCapabilitiesFailClosed(t *testing.T) {
	h, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/search", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/jobs/job/retry", strings.NewReader(`{"expected_job_version":1}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("retry route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/web-search", strings.NewReader(`{"query":"x","space_id":"space"}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("web search route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/web-fetch", strings.NewReader(`{"url":"https://example.com","space_id":"space"}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("web fetch route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/documents/batch-upload", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("batch upload route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/knowledge-bases/kb/feedback", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("feedback list route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/v1/knowledge-os/knowledge-bases/kb/grants/user", strings.NewReader(`{"role":"Viewer","expected_policy_version":1}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("grant route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/knowledge-bases/kb/grants", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("grant list route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/jobs", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("job list route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/feedback/feedback1/proposals", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("feedback proposal route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/knowledge-bases/kb1/documents", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("document upload route did not fail closed", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/spaces/space1/knowledge-bases", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("knowledge base create route did not fail closed", w.Code)
	}
}

func TestMetricsExposeRuntimeOnlyValues(t *testing.T) {
	h, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "memorys_goroutines") || !strings.Contains(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics response: %d %s", w.Code, w.Body.String())
	}
}

func TestConfiguredUploadIsMounted(t *testing.T) {
	store := &configuredUploadStore{}
	upload := &api.UploadHandler{
		Service: ingestion.UploadService{Store: store},
		Prepare: func(*http.Request) (contracts.BlobWriteRequest, error) {
			return contracts.BlobWriteRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, Size: 3, MediaType: "text/plain", IdempotencyKey: "upload"}, nil
		},
	}
	h, err := New(Config{Upload: upload})
	if err != nil {
		t.Fatal(err)
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
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || store.writes != 1 || !strings.Contains(w.Body.String(), "0123456789abcdef") {
		t.Fatalf("upload status=%d writes=%d body=%s", w.Code, store.writes, w.Body.String())
	}
}

func TestRuntimeOwnsOptionalRunnerLifecycle(t *testing.T) {
	r, err := NewRuntime(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = r.Ready(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w.Code != http.StatusOK || w.Body.String() != `{"status":"ready"}` {
		t.Fatalf("ready probe: %d %s", w.Code, w.Body.String())
	}
	if err = r.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = r.Ready(); err != nil {
		t.Fatal("runtime without runner should remain ready after stop")
	}
}

func TestRuntimeOwnsPublicationWorkerLifecycle(t *testing.T) {
	worker := &fakeWorker{}
	r, err := NewRuntime(Config{PublicationWorker: worker}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !worker.started || r.Ready() != nil {
		t.Fatal("publication worker was not started and admitted ready")
	}
	if err := r.Stop(); err != nil || !worker.stopped {
		t.Fatalf("publication worker was not stopped: %v", err)
	}
	if r.Ready() == nil {
		t.Fatal("stopped publication worker remained ready")
	}
}

func TestRuntimeOwnsParseWorkerLifecycle(t *testing.T) {
	worker := &fakeWorker{}
	r, err := NewRuntime(Config{ParseWorker: worker}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !worker.started || r.Ready() != nil {
		t.Fatal("parse worker was not started and admitted ready")
	}
	if err := r.Stop(); err != nil || !worker.stopped {
		t.Fatalf("parse worker was not stopped: %v", err)
	}
	if r.Ready() == nil {
		t.Fatal("stopped parse worker remained ready")
	}
}

func TestReadyCheckFailsClosed(t *testing.T) {
	checkErr := errors.New("database unavailable")
	h, err := New(Config{ReadyCheck: func(context.Context) error { return checkErr }})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status=%d body=%s", w.Code, w.Body.String())
	}
	h, err = New(Config{ReadyCheck: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w.Code != http.StatusOK || w.Body.String() != `{"status":"ready"}` {
		t.Fatalf("ready status=%d body=%s", w.Code, w.Body.String())
	}
}
