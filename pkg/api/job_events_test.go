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

type eventJobApp struct {
	cancel context.CancelFunc
}

func (a eventJobApp) GetJob(_ context.Context, in document.JobStatusInput) (contracts.ResourceJob, error) {
	if a.cancel != nil {
		a.cancel()
	}
	return contracts.ResourceJob{ResourceID: "doc-1", Job: contracts.Job{ID: in.JobID, State: "running", Stage: "document.parse", Attempt: 1, RowVersion: 2}}, nil
}

func TestJobEventsStreamsAuthorizedSnapshotAndReconnectID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/parse-jobs/job-1/events", nil).WithContext(ctx)
	r.SetPathValue("id", "job-1")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Last-Event-ID", "7")
	w := httptest.NewRecorder()
	api.JobEventsHandler{Service: eventJobApp{cancel: cancel}}.ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "id: 8") || !strings.Contains(body, "event: job") || !strings.Contains(body, `"state":"running"`) {
		t.Fatalf("status=%d body=%s", w.Code, body)
	}
}
