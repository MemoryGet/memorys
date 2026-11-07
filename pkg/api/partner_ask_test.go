package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type askApplication func(context.Context, api.PartnerAskInput, contracts.AnswerSink) error

func (f askApplication) Ask(c context.Context, in api.PartnerAskInput, sink contracts.AnswerSink) error {
	return f(c, in, sink)
}

func askResult(requestID contracts.ID) *contracts.AnswerResult {
	return &contracts.AnswerResult{RequestID: requestID, Answer: "答案 [1]", Citations: []contracts.Citation{{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}}, Status: "answered", ProfileVersion: 1, ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), TimingsMS: map[string]float64{"total": 1}, Degraded: []contracts.Degradation{}}
}

func TestPartnerAskRejectsInvalidRequestAndMapsJSONResult(t *testing.T) {
	h := api.PartnerAskHandler{Service: askApplication(func(_ context.Context, in api.PartnerAskInput, sink contracts.AnswerSink) error {
		return sink(contracts.AnswerEvent{Kind: "done", Result: askResult(in.Request.RequestID)})
	})}
	r := httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(`{"question":"问题","space_id":"space","stream":false}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"answered"`) || !strings.Contains(w.Body.String(), `"char_start"`) && strings.Contains(w.Body.String(), "char_range") {
		t.Fatalf("ask JSON response invalid: %d %s", w.Code, w.Body.String())
	}
	bad := httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(`{"question":"问题","space_id":"space","kb_ids":null}`))
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("Authorization", "Bearer token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, bad)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("null KB list accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestPartnerAskStreamsValidatedEvents(t *testing.T) {
	h := api.PartnerAskHandler{Service: askApplication(func(_ context.Context, in api.PartnerAskInput, sink contracts.AnswerSink) error {
		if err := sink(contracts.AnswerEvent{Kind: "meta"}); err != nil {
			return err
		}
		if err := sink(contracts.AnswerEvent{Kind: "answer_delta", Text: "答案 [1]"}); err != nil {
			return err
		}
		return sink(contracts.AnswerEvent{Kind: "done", Result: askResult(in.Request.RequestID)})
	})}
	r := httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(`{"question":"问题","space_id":"space","stream":true}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" || !strings.Contains(w.Body.String(), "event: answer_delta") || !strings.Contains(w.Body.String(), "event: done") {
		t.Fatalf("ask stream invalid: %d %s", w.Code, w.Body.String())
	}
}
