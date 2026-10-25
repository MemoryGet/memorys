package websearch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchSendsBearerAndDecodesResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tools/search" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"query":"go"`) {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"results":[{"rerank_score":2.4,"title":"Go","url":"https://go.dev","snippet":"snippet","summary":"summary","content":"# Go","authority":4}]}`)
	}))
	defer server.Close()
	c, err := NewClient("test-key", WithBaseURL(server.URL), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Search(context.Background(), SearchRequest{Query: "go", Count: 1, NeedContent: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 1 || out.Results[0].URL != "https://go.dev" || out.Results[0].Authority != 4 {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestFetchValidationAndDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"title":"Example","content":"# body"}`)
	}))
	defer server.Close()
	c, err := NewClient("key", WithBaseURL(server.URL), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Fetch(context.Background(), FetchRequest{URL: "https://example.com/a"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "Example" || out.Content != "# body" {
		t.Fatalf("unexpected response: %+v", out)
	}
	for _, req := range []FetchRequest{{URL: "ftp://example.com"}, {URL: "https://user:pass@example.com"}, {URL: "https://example.com" + strings.Repeat("x", 8200)}} {
		if _, err := c.Fetch(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("request %+v error = %v", req, err)
		}
	}
}

func TestSearchValidation(t *testing.T) {
	c, err := NewClient("key")
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []SearchRequest{{}, {Query: "   "}, {Query: "x", Count: 51}, {Query: "x", OriginalQuestion: ptr(" ")}} {
		if _, err := c.Search(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("request %+v error = %v", req, err)
		}
	}
}

func TestRetry429ThenSuccess(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{"results":[]}`)
	}))
	defer server.Close()
	c, err := NewClient("key", WithBaseURL(server.URL), WithMaxRetries(1))
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := c.Search(context.Background(), SearchRequest{Query: "x"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestUnauthorizedDoesNotExposeKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	c, err := NewClient("secret-key", WithBaseURL(server.URL), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Fetch(context.Background(), FetchRequest{URL: "https://example.com"})
	if !errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("error = %v", err)
	}
}

func ptr(s string) *string { return &s }
