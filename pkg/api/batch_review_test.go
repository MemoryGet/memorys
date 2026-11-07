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

type batchReviewFixture struct{}

func (batchReviewFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}
func (batchReviewFixture) DecideReview(context.Context, contracts.ReviewDecisionInput) (contracts.Approval, error) {
	return contracts.Approval{ID: "approval-1", RevisionID: "revision-1", Decision: "approve", RowVersion: 1}, nil
}

func TestBatchReviewHandlerReturnsPerItemResults(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/revisions:batch-approval-decisions", strings.NewReader(`{"items":[{"revision_id":"revision-1","decision":"approve","reason":"批量核验","expected_approval_version":0,"idempotency_key":"batch-1"}]}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(BatchReviewHandler{Service: batchReviewFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"committed"`) {
		t.Fatalf("batch response: %d %s", w.Code, w.Body.String())
	}
}
