package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type jobApp func(context.Context, document.JobStatusInput) (contracts.ResourceJob, error)

func (f jobApp) GetJob(c context.Context, q document.JobStatusInput) (contracts.ResourceJob, error) {
	return f(c, q)
}

func TestJobStatusProjectionHidesInternalBindingsAndRejectsInput(t *testing.T) {
	calls := 0
	var held []byte
	app := jobApp(func(_ context.Context, q document.JobStatusInput) (contracts.ResourceJob, error) {
		calls++
		held = q.Credential.Secret
		return contracts.ResourceJob{ResourceID: "actual-document", Job: contracts.Job{ID: q.JobID, SpaceID: "space", OperationID: "private-operation", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), FencingToken: 42, Stage: "document.delete.cleanup", State: "queued", RowVersion: 1}}, nil
	})
	mux := http.NewServeMux()
	mux.Handle("/jobs/{id}", api.JobStatusHandler{Service: app})
	for _, test := range []struct {
		method, path, body string
		status             int
	}{{"GET", "/jobs/job", "", 200}, {"GET", "/jobs/job?space_id=inject", "", 400}, {"GET", "/jobs/job", "{}", 400}, {"POST", "/jobs/job", "", 405}} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("Authorization", "Bearer token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, request)
		if w.Code != test.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		if w.Code == 200 {
			var result map[string]any
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result) != 8 || result["resource_id"] != "actual-document" || strings.Contains(w.Body.String(), "private-operation") || strings.Contains(w.Body.String(), "FencingToken") {
				t.Fatal("job projection leaked internal metadata")
			}
		}
	}
	if calls != 1 || string(held) != strings.Repeat("\x00", 5) {
		t.Fatal("invalid request reached app or credential not cleared")
	}
}
