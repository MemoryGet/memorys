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

type draftEditFixture struct{ conflict bool }

func (draftEditFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}

func (f draftEditFixture) EditDraft(_ context.Context, input contracts.DraftEditInput) (contracts.Draft, error) {
	if f.conflict {
		return contracts.Draft{}, contracts.ErrDraftEditConflict
	}
	return contracts.Draft{ID: input.DraftID, DocumentID: "document-1", RowVersion: input.ExpectedVersion + 1, State: "draft", Blocks: input.Blocks}, nil
}

func TestDraftEditHandlerReturnsUpdatedDraft(t *testing.T) {
	body := `{"expected_version":3,"blocks":[{"id":"block-1","kind":"text","text":"已保存","draft_id":"draft-1","row_version":1,"source_anchor":{"kind":"revision","revision_block_id":"block-1","locator":{"precision":"coarse"}}}]}`
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/knowledge-os/drafts/draft-1", strings.NewReader(body))
	r.SetPathValue("draft_id", "draft-1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "edit-1")
	w := httptest.NewRecorder()
	(DraftEditHandler{Service: draftEditFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"row_version":4`) || !strings.Contains(w.Body.String(), `"text":"已保存"`) {
		t.Fatalf("draft edit response: %d %s", w.Code, w.Body.String())
	}
}

func TestDraftEditHandlerMapsConflict(t *testing.T) {
	body := `{"expected_version":3,"blocks":[{"id":"block-1","kind":"text","text":"已保存","draft_id":"draft-1","row_version":1,"source_anchor":{"kind":"revision","revision_block_id":"block-1","locator":{"precision":"coarse"}}}]}`
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/knowledge-os/drafts/draft-1", strings.NewReader(body))
	r.SetPathValue("draft_id", "draft-1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "edit-2")
	w := httptest.NewRecorder()
	(DraftEditHandler{Service: draftEditFixture{conflict: true}}).ServeHTTP(w, r)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"conflict"`) {
		t.Fatalf("conflict response: %d %s", w.Code, w.Body.String())
	}
}

func TestDraftEditHandlerRejectsUnknownFields(t *testing.T) {
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/knowledge-os/drafts/draft-1", strings.NewReader(`{"expected_version":3,"blocks":[],"extra":1}`))
	r.SetPathValue("draft_id", "draft-1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "edit-3")
	w := httptest.NewRecorder()
	(DraftEditHandler{Service: draftEditFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown draft edit field accepted: %d %s", w.Code, w.Body.String())
	}
}
