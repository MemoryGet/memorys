package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/websearch"
)

type webSearchApp struct{}

func (webSearchApp) Search(context.Context, websearch.SearchInput) (websearch.SearchResponse, error) {
	return websearch.SearchResponse{Results: []websearch.SearchResult{{Title: "Result", URL: "https://example.com"}}}, nil
}
func (webSearchApp) Fetch(context.Context, websearch.FetchInput) (websearch.FetchResponse, error) {
	return websearch.FetchResponse{Title: "Page", Content: "# body"}, nil
}

func TestWebSearchHandlerRequiresBearerAndRejectsUnknownFields(t *testing.T) {
	h := WebSearchHandler{Service: webSearchApp{}}
	for _, tc := range []struct {
		name   string
		auth   string
		body   string
		status int
	}{
		{"missing auth", "", `{"query":"x","count":1,"space_id":"space"}`, http.StatusUnauthorized},
		{"unknown field", "Bearer token", `{"query":"x","count":1,"space_id":"space","extra":true}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/web-search", strings.NewReader(tc.body))
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestWebSearchAndFetchHandlersReturnJSON(t *testing.T) {
	search := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/web-search", strings.NewReader(`{"query":"x","count":1,"space_id":"space"}`))
	r.Header.Set("Authorization", "Bearer token")
	(WebSearchHandler{Service: webSearchApp{}}).ServeHTTP(search, r)
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), `"title":"Result"`) {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}

	fetch := httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/v1/web-fetch", strings.NewReader(`{"url":"https://example.com","space_id":"space"}`))
	r.Header.Set("Authorization", "Bearer token")
	(WebFetchHandler{Service: webSearchApp{}}).ServeHTTP(fetch, r)
	if fetch.Code != http.StatusOK || !strings.Contains(fetch.Body.String(), `"content":"# body"`) {
		t.Fatalf("fetch status=%d body=%s", fetch.Code, fetch.Body.String())
	}
}
