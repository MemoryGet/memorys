package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type publicationStartFixture struct{}

func (publicationStartFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}

func (publicationStartFixture) StartPublication(context.Context, contracts.PublicationStartInput) (contracts.PublicationStartResult, error) {
	return contracts.PublicationStartResult{Operation: contracts.PublicationOperation{ID: "operation-1", DocumentID: "document-1", RevisionID: "revision-1", ApprovalID: "approval-1", ExpectedEpoch: 2, State: "preparing"}, Job: contracts.Job{ID: "job-1"}}, nil
}

func TestPublicationStartHandlerReturnsPreparingOperation(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/documents/document-1/publications", strings.NewReader(`{"revision_id":"revision-1","approval_id":"approval-1","expected_epoch":2}`))
	r.SetPathValue("id", "document-1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "publish-1")
	w := httptest.NewRecorder()
	(PublicationStartHandler{Service: publicationStartFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"operation_id":"operation-1"`) || !strings.Contains(w.Body.String(), `"job_id":"job-1"`) {
		t.Fatalf("publication response: %d %s", w.Code, w.Body.String())
	}
}
