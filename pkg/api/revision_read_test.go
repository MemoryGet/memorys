package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type revisionReadFixture struct{}

func (revisionReadFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}

func (revisionReadFixture) ReadRevision(context.Context, contracts.RevisionReadInput) (contracts.Revision, error) {
	draft := contracts.Draft{ID: "projection", DocumentID: "document-1", RowVersion: 1, State: "draft", Blocks: []contracts.DraftBlock{{ID: "block-1", DraftID: "projection", RowVersion: 1, Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: "冻结内容", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block-1", Locator: contracts.Locator{Precision: "coarse"}}}}}}
	frozen, _ := document.BuildFrozenContent(draft)
	return contracts.Revision{ID: "revision-1", DocumentID: "document-1", ContentHash: frozen.ContentHash, BlocksHash: frozen.BlocksHash, HashFormat: frozen.HashFormat, CreatedBy: "reviewer", CreatedAt: frozenTime(), Blocks: []contracts.Block{{ID: "block-1", RevisionID: "revision-1", Order: 0, Content: draft.Blocks[0].Content}}}, nil
}

func frozenTime() (t time.Time) { return time.Unix(10, 0).UTC() }

func TestRevisionReadHandlerReturnsFrozenBlocks(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/revisions/revision-1", nil)
	r.SetPathValue("revision_id", "revision-1")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(RevisionReadHandler{Service: revisionReadFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"revision_id":"revision-1"`) || !strings.Contains(w.Body.String(), `"text":"冻结内容"`) {
		t.Fatalf("revision response: %d %s", w.Code, w.Body.String())
	}
}
