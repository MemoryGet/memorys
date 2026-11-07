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

type feedbackProposalFixture struct{}

func (feedbackProposalFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}
func (feedbackProposalFixture) ProposeFeedback(context.Context, contracts.FeedbackProposalInput) (contracts.FeedbackProposal, error) {
	return contracts.FeedbackProposal{ID: "proposal1", FeedbackID: "feedback1", DraftID: "draft1", State: "proposed", CreatedAt: time.Unix(1, 0).UTC()}, nil
}

func TestFeedbackProposalHandlerAcceptsStrictRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/feedback/feedback1/proposals", strings.NewReader(`{"comment":"请修订这段内容"}`))
	r.SetPathValue("id", "feedback1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "proposal-key")
	w := httptest.NewRecorder()
	(FeedbackProposalHandler{Service: feedbackProposalFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"draft_id":"draft1"`) || !strings.Contains(w.Body.String(), `"state":"proposed"`) {
		t.Fatalf("proposal response: %d %s", w.Code, w.Body.String())
	}
}

func TestFeedbackProposalHandlerRejectsUnknownFields(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/feedback/feedback1/proposals", strings.NewReader(`{"comment":"x","extra":true}`))
	r.SetPathValue("id", "feedback1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "proposal-key")
	w := httptest.NewRecorder()
	(FeedbackProposalHandler{Service: feedbackProposalFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field accepted: %d %s", w.Code, w.Body.String())
	}
}
