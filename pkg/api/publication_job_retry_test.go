package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemoryGet/memorys/pkg/api"
)

func TestPublicationJobRetryFailsClosedWhenUnconfigured(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/publication-jobs/job/retry", nil)
	api.PublicationJobRetryHandler{}.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
