package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

type application func(context.Context, knowledge.SearchInput) ([]contracts.Candidate, error)

func (f application) Search(c context.Context, r knowledge.SearchInput) ([]contracts.Candidate, error) {
	return f(c, r)
}
func TestApplicationSearchRejectsCredentialsAndInternalArguments(t *testing.T) {
	calls := 0
	h := api.ApplicationSearchHandler{Service: application(func(context.Context, knowledge.SearchInput) ([]contracts.Candidate, error) { calls++; return nil, nil })}
	for _, test := range []struct {
		url     string
		headers []string
		status  int
	}{{"/search?q=x&space_id=s&kb_id=k", nil, 401}, {"/search?q=x&space_id=s&kb_id=k", []string{"Bearer a", "Bearer b"}, 401}, {"/search?q=x&space_id=s&kb_id=k", []string{"Bearer a,b"}, 401}, {"/search?q=x&space_id=s&kb_id=k&scope_id=admin", []string{"Bearer good"}, 400}, {"/search?q=x&q=y&space_id=s&kb_id=k", []string{"Bearer good"}, 400}, {"/search?q=x&space_id=s&kb_id=k&limit=1001", []string{"Bearer good"}, 400}, {"/search?q=x&space_id=s&kb_id=k&limit=%zz", []string{"Bearer good"}, 400}} {
		r := httptest.NewRequest(http.MethodGet, test.url, nil)
		for _, header := range test.headers {
			r.Header.Add("Authorization", header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatal(test, w.Code)
		}
	}
	if calls != 0 {
		t.Fatal("invalid request reached application")
	}
}
func TestApplicationSearchProjectsPublicResultsAndClearsCredential(t *testing.T) {
	var scratch []byte
	h := api.ApplicationSearchHandler{Service: application(func(c context.Context, in knowledge.SearchInput) ([]contracts.Candidate, error) {
		scratch = in.Credential.Secret
		if in.Credential.Kind != "access_token" || string(scratch) != "secret" || in.RequestID == "" || in.Limit != 10 || in.SpaceID != "space" || len(in.KnowledgeBaseIDs) != 1 {
			t.Fatal("public input binding lost")
		}
		if _, ok := c.Deadline(); !ok {
			t.Fatal("unbounded application")
		}
		return []contracts.Candidate{{Text: "verified", Rank: 1, RetrieverID: "private-backend", Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}}}, nil
	})}
	r := httptest.NewRequest(http.MethodGet, "/search?q=x&space_id=space&kb_id=kb", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "verified") || strings.Contains(w.Body.String(), "private-backend") || strings.Contains(w.Body.String(), "secret") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, b := range scratch {
		if b != 0 {
			t.Fatal("transport credential not cleared")
		}
	}
	h.Service = application(func(context.Context, knowledge.SearchInput) ([]contracts.Candidate, error) {
		return []contracts.Candidate{{Text: "protected"}}, errors.New("private failure")
	})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "protected") {
		t.Fatal("failed output leaked")
	}
}
