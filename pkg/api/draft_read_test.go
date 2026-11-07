package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type draftReadFixture struct{}

func (draftReadFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}

func (draftReadFixture) ReadDraft(_ context.Context, input contracts.DraftReadInput) (contracts.Draft, error) {
	return contracts.Draft{ID: input.DraftID, DocumentID: "document-1", RowVersion: 3, State: "draft", Blocks: []contracts.DraftBlock{{ID: "block-1", DraftID: input.DraftID, RowVersion: 7, Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: "可编辑正文", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block-1", Locator: contracts.Locator{Precision: "coarse"}}}}}}, nil
}

func TestDraftReadHandlerReturnsVersionedBlocks(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/drafts/draft-1", nil)
	r.SetPathValue("draft_id", "draft-1")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(DraftReadHandler{Service: draftReadFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"row_version":3`) || !strings.Contains(w.Body.String(), `"draft_id":"draft-1"`) || !strings.Contains(w.Body.String(), `"row_version":7`) {
		t.Fatalf("draft response: %d %s", w.Code, w.Body.String())
	}
}

func TestDraftReadHandlerRejectsQueryAndUnknownBearer(t *testing.T) {
	for _, target := range []string{"/api/v1/knowledge-os/drafts/draft-1?x=1", "/api/v1/knowledge-os/drafts/draft-1"} {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		r.SetPathValue("draft_id", "draft-1")
		if strings.Contains(target, "?") {
			r.Header.Set("Authorization", "Bearer token")
		} else {
			r.Header.Set("Authorization", "Basic token")
		}
		w := httptest.NewRecorder()
		(DraftReadHandler{Service: draftReadFixture{}}).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest && w.Code != http.StatusUnauthorized {
			t.Fatalf("invalid draft request accepted: %d %s", w.Code, w.Body.String())
		}
	}
}
