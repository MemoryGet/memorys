package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type knowledgeBaseListFixture struct{}

func (knowledgeBaseListFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}
func (knowledgeBaseListFixture) ListKnowledgeBases(context.Context, contracts.KnowledgeBaseListInput) (contracts.KnowledgeBasePage, error) {
	return contracts.KnowledgeBasePage{Items: []contracts.KnowledgeBase{{ID: "kb-1", SpaceID: "space-1", Name: "知识库", RowVersion: 1, PolicyVersion: 2}}}, nil
}

func TestKnowledgeBaseListHandlerReturnsItems(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/spaces/space-1/knowledge-bases", nil)
	r.SetPathValue("space_id", "space-1")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(KnowledgeBaseListHandler{Service: knowledgeBaseListFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kb-1"`) || !strings.Contains(w.Body.String(), `"policy_version":2`) {
		t.Fatalf("kb response: %d %s", w.Code, w.Body.String())
	}
}
