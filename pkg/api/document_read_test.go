package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type documentReadFixture struct{}

func (documentReadFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}

func (documentReadFixture) ReadDocument(context.Context, contracts.DocumentReadInput) (contracts.Document, error) {
	return contracts.Document{ID: "document", SpaceID: "space", KnowledgeBaseID: "kb", Title: "资料", RowVersion: 2, PublicationEpoch: 1}, nil
}

func TestDocumentReadHandlerReturnsMetadata(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/documents/document", nil)
	r.SetPathValue("id", "document")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(DocumentReadHandler{Service: documentReadFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"document"`) || !strings.Contains(w.Body.String(), `"title":"资料"`) {
		t.Fatalf("document response: %d %s", w.Code, w.Body.String())
	}
}
