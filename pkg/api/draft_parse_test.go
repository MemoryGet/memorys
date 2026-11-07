package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type draftParseFixture struct{}

func (draftParseFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}
func (draftParseFixture) ParseDraft(context.Context, contracts.DraftParseInput) (contracts.DraftParseResult, error) {
	return contracts.DraftParseResult{DocumentID: "document-1", Job: contracts.Job{ID: "parse-job-1", SpaceID: "space-1", State: "queued", Stage: "document.parse", Attempt: 1, RowVersion: 1}}, nil
}

func TestDraftParseHandlerReturnsQueuedJob(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/drafts/draft-1/parse", strings.NewReader(`{"expected_version":3,"pipeline_profile_id":"pipeline-1","pipeline_profile_version":1}`))
	r.SetPathValue("draft_id", "draft-1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "parse-1")
	w := httptest.NewRecorder()
	(DraftParseHandler{Service: draftParseFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"id":"parse-job-1"`) || !strings.Contains(w.Body.String(), `"resource_id":"document-1"`) {
		t.Fatalf("parse response: %d %s", w.Code, w.Body.String())
	}
}
