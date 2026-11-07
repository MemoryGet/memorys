package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type draftSubmitFixture struct{}

func (draftSubmitFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}

func (draftSubmitFixture) SubmitDraft(ctx context.Context, input contracts.DraftSubmitInput) (contracts.SubmitResult, error) {
	revision, err := (revisionReadFixture{}).ReadRevision(ctx, contracts.RevisionReadInput{RevisionID: "revision-1"})
	if err != nil {
		return contracts.SubmitResult{}, err
	}
	revision.ID = "revision-submitted"
	for i := range revision.Blocks {
		revision.Blocks[i].RevisionID = revision.ID
	}
	return contracts.SubmitResult{Revision: revision, ApprovalRequestID: "approval-1"}, nil
}

func TestDraftSubmitHandlerReturnsSubmission(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/drafts/draft-1/submit", strings.NewReader(`{"expected_version":3}`))
	r.SetPathValue("draft_id", "draft-1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "submit-1")
	w := httptest.NewRecorder()
	(DraftSubmitHandler{Service: draftSubmitFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"approval_request_id":"approval-1"`) || !strings.Contains(w.Body.String(), `"revision"`) {
		t.Fatalf("submit response: %d %s", w.Code, w.Body.String())
	}
}
