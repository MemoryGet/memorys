package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type publicationWithdrawFixture struct{}

func (publicationWithdrawFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}

func (publicationWithdrawFixture) WithdrawPublication(context.Context, contracts.PublicationWithdrawInput) (contracts.WithdrawResult, error) {
	return contracts.WithdrawResult{DocumentID: "document-1", PublicationEpoch: 3, HistoryID: "history-1"}, nil
}

func TestPublicationWithdrawHandlerReturnsNewEpoch(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/documents/document-1/withdraw", strings.NewReader(`{"expected_epoch":2,"reason":"下线过期版本"}`))
	r.SetPathValue("id", "document-1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "withdraw-1")
	w := httptest.NewRecorder()
	(PublicationWithdrawHandler{Service: publicationWithdrawFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"publication_epoch":3`) || !strings.Contains(w.Body.String(), `"history_id":"history-1"`) {
		t.Fatalf("withdraw response: %d %s", w.Code, w.Body.String())
	}
}
