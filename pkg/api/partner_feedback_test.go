package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type feedbackApplication func(context.Context, api.PartnerFeedbackInput) (contracts.FeedbackResult, error)

func (f feedbackApplication) SubmitFeedback(c context.Context, in api.PartnerFeedbackInput) (contracts.FeedbackResult, error) {
	return f(c, in)
}

func TestPartnerFeedbackRejectsUnconfiguredAndAmbiguousTargets(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	(api.PartnerFeedbackHandler{}).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured status = %d", w.Code)
	}
	body := `{"space_id":"space","citation":{"document_id":"doc","revision_id":"rev","block_id":"block","source_anchor":{"kind":"revision","revision_block_id":"block","locator":{"precision":"coarse"}}},"target_request_id":"request","kind":"incorrect","comment":"bad","idempotency_key":"key"}`
	r = httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	w = httptest.NewRecorder()
	(api.PartnerFeedbackHandler{Service: feedbackApplication(func(context.Context, api.PartnerFeedbackInput) (contracts.FeedbackResult, error) {
		t.Fatal("ambiguous feedback reached service")
		return contracts.FeedbackResult{}, nil
	})}).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous target status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestPartnerFeedbackMapsCitationAndReturnsSafeResult(t *testing.T) {
	var captured api.PartnerFeedbackInput
	h := api.PartnerFeedbackHandler{Service: feedbackApplication(func(_ context.Context, in api.PartnerFeedbackInput) (contracts.FeedbackResult, error) {
		captured = in
		return contracts.FeedbackResult{FeedbackID: "feedback-1", State: "received"}, nil
	})}
	body := `{"space_id":"space","citation":{"document_id":"doc","revision_id":"rev","block_id":"block","source_anchor":{"kind":"revision","revision_block_id":"block","locator":{"precision":"exact","page":2}}},"kind":"helpful","comment":"可核对","idempotency_key":"key-1"}`
	r := httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || captured.SpaceID != "space" || captured.Citation == nil || captured.Citation.Source.Locator.Page == nil || *captured.Citation.Source.Locator.Page != 2 || captured.Credential.Kind != "access_token" {
		t.Fatalf("feedback mapping failed: status=%d body=%s input=%#v", w.Code, w.Body.String(), captured)
	}
	var result map[string]any
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result["feedback_id"] != "feedback-1" || result["state"] != "received" {
		t.Fatalf("unexpected feedback response: %s", w.Body.String())
	}
}
