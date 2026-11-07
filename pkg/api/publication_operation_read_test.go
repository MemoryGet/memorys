package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type publicationOperationFixture struct{}

func (publicationOperationFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}
func (publicationOperationFixture) ReadPublicationOperation(context.Context, contracts.PublicationOperationReadInput) (contracts.PublicationOperation, error) {
	return contracts.PublicationOperation{ID: "operation-1", DocumentID: "document-1", RevisionID: "revision-1", ApprovalID: "approval-1", State: "preparing"}, nil
}

func TestPublicationOperationReadHandlerReturnsOperation(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/publication-operations/operation-1", nil)
	r.SetPathValue("id", "operation-1")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(PublicationOperationReadHandler{Service: publicationOperationFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"operation_id":"operation-1"`) || !strings.Contains(w.Body.String(), `"state":"preparing"`) {
		t.Fatalf("operation response: %d %s", w.Code, w.Body.String())
	}
}
