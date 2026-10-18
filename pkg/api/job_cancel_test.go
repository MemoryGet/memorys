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

type cancelApp func(context.Context, document.JobCancellationInput) (contracts.ResourceJob, error)

func (f cancelApp) CancelJob(c context.Context, q document.JobCancellationInput) (contracts.ResourceJob, error) {
	return f(c, q)
}

func TestJobCancellationHandlerValidatesCommandBeforeApplication(t *testing.T) {
	calls := 0
	h := api.JobCancellationHandler{Service: cancelApp(func(context.Context, document.JobCancellationInput) (contracts.ResourceJob, error) {
		calls++
		return contracts.ResourceJob{ResourceID: "doc", Job: contracts.Job{ID: "job"}}, nil
	})}
	mux := http.NewServeMux()
	mux.Handle("/jobs/{id}/cancel", h)
	for _, tc := range []struct {
		method, body string
		status       int
	}{
		{http.MethodGet, `{"expected_job_version":1}`, 405},
		{http.MethodPost, `{}`, 400},
		{http.MethodPost, `{"expected_job_version":1}`, 401},
	} {
		r := httptest.NewRequest(tc.method, "/jobs/job/cancel", strings.NewReader(tc.body))
		r.Header.Set("Idempotency-Key", "0123456789abcdef")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if calls != 0 {
		t.Fatal("invalid cancellation reached application")
	}
}
