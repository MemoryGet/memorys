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
)

type feedbackListFixture struct{}

func (feedbackListFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}

func (feedbackListFixture) ListFeedback(context.Context, contracts.FeedbackListInput) (contracts.FeedbackPage, error) {
	return contracts.FeedbackPage{Items: []contracts.FeedbackItem{{ID: "feedback", SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "document", RevisionID: "revision", BlockID: "block", Kind: "incorrect", Comment: "引用不准确", State: "received", CreatedAt: time.Unix(10, 0).UTC()}}}, nil
}

func TestFeedbackListHandlerReturnsItems(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/knowledge-bases/kb/feedback?page_size=10", nil)
	r.SetPathValue("kb_id", "kb")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(FeedbackListHandler{Service: feedbackListFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"document_id":"document"`) || !strings.Contains(w.Body.String(), `"kind":"incorrect"`) {
		t.Fatalf("feedback list response: %d %s", w.Code, w.Body.String())
	}
}
