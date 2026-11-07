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

type approvalListFixture struct{}

func (approvalListFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}

func (approvalListFixture) ListApprovalRequests(context.Context, contracts.ApprovalRequestListInput) (contracts.ApprovalRequestPage, error) {
	return contracts.ApprovalRequestPage{Items: []contracts.ApprovalRequest{{ID: "request", RevisionID: "revision", DocumentID: "document", SubmittedBy: "author", SubmittedAt: time.Unix(10, 0).UTC(), State: "submitted", ContentHash: contracts.Digest(strings.Repeat("a", 64)), PolicyVersion: 1}}}, nil
}

func TestApprovalRequestListHandlerReturnsItems(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/knowledge-bases/kb/approval-requests?page_size=10", nil)
	r.SetPathValue("kb_id", "kb")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(ApprovalRequestListHandler{Service: approvalListFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"document_id":"document"`) || !strings.Contains(w.Body.String(), `"state":"submitted"`) {
		t.Fatalf("approval list response: %d %s", w.Code, w.Body.String())
	}
}
