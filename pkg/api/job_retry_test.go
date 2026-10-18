package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type retryApp func(context.Context, document.JobRetryInput) (contracts.ResourceJob, error)

func (f retryApp) RetryJob(c context.Context, q document.JobRetryInput) (contracts.ResourceJob, error) {
	return f(c, q)
}

func TestJobRetryHandlerValidatesCommandBeforeApplication(t *testing.T) {
	calls := 0
	h := api.JobRetryHandler{Service: retryApp(func(context.Context, document.JobRetryInput) (contracts.ResourceJob, error) {
		calls++
		return contracts.ResourceJob{ResourceID: "doc", Job: contracts.Job{ID: "job"}}, nil
	})}
	mux := http.NewServeMux()
	mux.Handle("/jobs/{id}/retry", h)
	for _, tc := range []struct {
		method, body string
		status       int
	}{
		{http.MethodGet, `{"expected_job_version":1}`, http.StatusMethodNotAllowed},
		{http.MethodPost, `{}`, http.StatusBadRequest},
		{http.MethodPost, `{"expected_job_version":1}`, http.StatusUnauthorized},
	} {
		r := httptest.NewRequest(tc.method, "/jobs/job/retry", strings.NewReader(tc.body))
		r.Header.Set("Idempotency-Key", "0123456789abcdef")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if calls != 0 {
		t.Fatal("invalid retry reached application")
	}
}
