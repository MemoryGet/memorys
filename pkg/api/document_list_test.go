package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type documentListFixture struct{}

func (documentListFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}
func (documentListFixture) ListDocuments(context.Context, contracts.DocumentListInput) (contracts.DocumentPage, error) {
	return contracts.DocumentPage{Items: []contracts.Document{{ID: "document-1", SpaceID: "space-1", KnowledgeBaseID: "kb-1", Title: "文档", RowVersion: 2, PublicationEpoch: 1}}}, nil
}

func TestDocumentListHandlerReturnsDocumentProjection(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/knowledge-bases/kb-1/documents", nil)
	r.SetPathValue("kb_id", "kb-1")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(DocumentListHandler{Service: documentListFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"document-1"`) || !strings.Contains(w.Body.String(), `"title":"文档"`) {
		t.Fatalf("documents response: %d %s", w.Code, w.Body.String())
	}
}
