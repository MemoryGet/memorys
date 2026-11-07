package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type draftCreateFixture struct{}

func (draftCreateFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}
func (draftCreateFixture) CreateDraft(context.Context, contracts.DraftCreateInput) (contracts.Draft, error) {
	return contracts.Draft{ID: "draft-new", DocumentID: "document-1", RowVersion: 1, State: "draft"}, nil
}

func TestDraftCreateHandlerReturnsDraft(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/documents/document-1/drafts", strings.NewReader(`{"base_revision_id":null,"expected_document_version":2}`))
	r.SetPathValue("id", "document-1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "draft-1")
	w := httptest.NewRecorder()
	(DraftCreateHandler{Service: draftCreateFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"id":"draft-new"`) {
		t.Fatalf("draft create response: %d %s", w.Code, w.Body.String())
	}
}
