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

type jobListFixture struct{}

func (jobListFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}

func (jobListFixture) ListJobs(context.Context, contracts.JobListInput) (contracts.JobPage, error) {
	return contracts.JobPage{Items: []contracts.JobListItem{{Kind: "parse", ResourceJob: contracts.ResourceJob{ResourceID: "document", Job: contracts.Job{ID: "job", SpaceID: "space", Stage: "document.parse", State: "queued", RowVersion: 1}}}}}, nil
}

func TestJobListHandlerReturnsFilteredItems(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/jobs?kind=parse&page_size=10", nil)
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(JobListHandler{Service: jobListFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kind":"parse"`) || !strings.Contains(w.Body.String(), `"resource_id":"document"`) {
		t.Fatalf("job list response: %d %s", w.Code, w.Body.String())
	}
}

func TestJobListHandlerAcceptsPausedAndTimeFilters(t *testing.T) {
	called := false
	service := jobListFixtureWithInput(func(input contracts.JobListInput) {
		called = true
		if input.State != "paused" || input.CreatedAfter.IsZero() || input.CreatedBefore.IsZero() || !input.CreatedAfter.Before(input.CreatedBefore) {
			t.Fatalf("filters not bound: %+v", input)
		}
	})
	r := httptest.NewRequest(http.MethodGet, "/jobs?state=paused&created_after=2026-10-01T00:00:00Z&created_before=2026-11-01T00:00:00Z", nil)
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	(JobListHandler{Service: service}).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !called {
		t.Fatalf("paused/time filter response: %d %s", w.Code, w.Body.String())
	}
}

type jobListFixtureWithInput func(contracts.JobListInput)

func (f jobListFixtureWithInput) ProfileIdentity() (uint64, contracts.Digest, error) {
	h := sha256.Sum256([]byte("profile"))
	return 1, contracts.Digest(hex.EncodeToString(h[:])), nil
}

func (f jobListFixtureWithInput) ListJobs(_ context.Context, input contracts.JobListInput) (contracts.JobPage, error) {
	f(input)
	return contracts.JobPage{}, nil
}
