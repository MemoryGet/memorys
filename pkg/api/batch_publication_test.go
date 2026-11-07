package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type batchPublicationFixture struct{}

func (batchPublicationFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}
func (batchPublicationFixture) StartPublication(context.Context, contracts.PublicationStartInput) (contracts.PublicationStartResult, error) {
	return contracts.PublicationStartResult{Operation: contracts.PublicationOperation{ID: "operation-1", DocumentID: "document-1", State: "preparing"}, Job: contracts.Job{ID: "job-1"}}, nil
}

func TestBatchPublicationHandlerReturnsAcceptedItem(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/publication-operations:batch-start", strings.NewReader(`{"items":[{"document_id":"document-1","revision_id":"revision-1","approval_id":"approval-1","expected_epoch":0,"idempotency_key":"publish-1"}]}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(BatchPublicationHandler{Service: batchPublicationFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"accepted"`) {
		t.Fatalf("batch publication response: %d %s", w.Code, w.Body.String())
	}
}
