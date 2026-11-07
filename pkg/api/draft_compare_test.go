package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type draftCompareFixture struct{}

func (draftCompareFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}
func (draftCompareFixture) CompareDraft(context.Context, contracts.DraftCompareInput) (contracts.DraftCompareResult, error) {
	return contracts.DraftCompareResult{DraftID: "draft-1", FromVersion: 2, ToVersion: 3, Changes: []contracts.DraftCompareChange{{BlockID: "block-1", Kind: "changed", BeforeVersion: 1, AfterVersion: 2}}}, nil
}

func TestDraftCompareHandlerReturnsChanges(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/drafts/draft-1/compare?from_version=2", nil)
	r.SetPathValue("draft_id", "draft-1")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(DraftCompareHandler{Service: draftCompareFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kind":"changed"`) {
		t.Fatalf("compare response: %d %s", w.Code, w.Body.String())
	}
}
