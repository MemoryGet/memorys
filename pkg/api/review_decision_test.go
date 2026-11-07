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

type reviewAppFixture struct{}

func (reviewAppFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}

func (reviewAppFixture) DecideReview(_ context.Context, input ReviewDecisionInput) (contracts.Approval, error) {
	return contracts.Approval{ID: "approval", RevisionID: input.RevisionID, Decision: input.Decision, Reason: input.Reason, ActorID: "reviewer", ContentHash: contracts.Digest(strings.Repeat("a", 64)), PolicyVersion: 1, RowVersion: 1, DecidedAt: time.Unix(10, 0).UTC()}, nil
}

func TestReviewDecisionHandlerReturnsApproval(t *testing.T) {
	h := ReviewDecisionHandler{Service: reviewAppFixture{}}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/revisions/revision-1/approval-decisions", strings.NewReader(`{"decision":"approve","reason":"已核对","expected_approval_version":0}`))
	r.SetPathValue("revision_id", "revision-1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "review-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"decision":"approve"`) || !strings.Contains(w.Body.String(), `"revision_id":"revision-1"`) {
		t.Fatalf("review response: %d %s", w.Code, w.Body.String())
	}
}

func TestReviewDecisionHandlerRejectsUnknownFieldsAndMissingAuth(t *testing.T) {
	for _, body := range []string{`{"decision":"approve","reason":"x","expected_approval_version":0,"extra":1}`, `{"decision":"hold","reason":"x","expected_approval_version":0}`} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/revisions/revision-1/approval-decisions", strings.NewReader(body))
		r.SetPathValue("revision_id", "revision-1")
		r.Header.Set("Content-Type", "application/json")
		if body == `{"decision":"hold","reason":"x","expected_approval_version":0}` {
			r.Header.Set("Authorization", "Bearer token")
			r.Header.Set("Idempotency-Key", "review-2")
		}
		w := httptest.NewRecorder()
		(ReviewDecisionHandler{Service: reviewAppFixture{}}).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest && w.Code != http.StatusUnauthorized {
			t.Fatalf("invalid review request accepted: %d %s", w.Code, w.Body.String())
		}
	}
}
