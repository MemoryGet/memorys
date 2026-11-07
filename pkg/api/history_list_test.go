package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type historyListFixture struct{}

func (historyListFixture) ProfileIdentity() (uint64, contracts.Digest, error) { return revisionReadFixture{}.ProfileIdentity() }
func (historyListFixture) ListPublicationHistory(context.Context, contracts.PublicationHistoryListInput) (contracts.PublicationHistoryPage, error) {
	return contracts.PublicationHistoryPage{Items: []contracts.PublicationHistory{{ID: "history-1", DocumentID: "document-1", PublicationEpoch: 2, Action: "publish", ActorID: "actor-1", Reason: "发布", OccurredAt: time.Unix(10, 0).UTC()} }}, nil
}

func TestPublicationHistoryListHandlerReturnsItems(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/documents/document-1/history", nil)
	r.SetPathValue("id", "document-1")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(PublicationHistoryListHandler{Service: historyListFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"history-1"`) || !strings.Contains(w.Body.String(), `"publication_epoch":2`) {
		t.Fatalf("history response: %d %s", w.Code, w.Body.String())
	}
}
