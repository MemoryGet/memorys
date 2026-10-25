package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

type fakeRead struct{}

func (fakeRead) ReadCurrent(context.Context, knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error) {
	return contracts.FrozenContentRecord{}, nil
}

type fakeDelete struct{}

func (fakeDelete) DeleteDocument(context.Context, document.AuthenticatedDeleteInput) (contracts.DeleteDocumentResult, error) {
	return contracts.DeleteDocumentResult{}, nil
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
