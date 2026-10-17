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
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

func (application) ProfileIdentity() (uint64, contracts.Digest, error) {
	return 1, contracts.Digest(strings.Repeat("a", 64)), nil
}
func TestPartnerSearchStrictBodyAndTopK(t *testing.T) {
	calls := 0
	app := application(func(context.Context, knowledge.SearchInput) ([]contracts.Candidate, error) { calls++; return nil, nil })
	for _, body := range []string{`{"query":"x","query":"y","space_id":"s"}`, `{"Query":"x","space_id":"s"}`, `{"query":"x","space_id":"s","scope_id":"admin"}`, `{"query":"x","space_id":"s","kb_ids":null}`, `{"query":"x","space_id":"s","kb_ids":["k","k"]}`, `{"query":"x","space_id":"s","top_k":null}`, `{"query":"x","space_id":"s","top_k":1.00000000000000000000001}`, `{"query":"x","space_id":"s","top_k":101}`} {
		r := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		(api.PartnerSearchHandler{Service: app}).ServeHTTP(w, r)
		if w.Code != 400 || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
	if calls != 0 {
		t.Fatal("invalid wire request reached application")
	}
}
func TestPartnerSearchPreservesOmittedAndEmptyKnowledgeBases(t *testing.T) {
	var captured []byte
	app := application(func(_ context.Context, in knowledge.SearchInput) ([]contracts.Candidate, error) {
		captured = in.Credential.Secret
		if in.Limit != 1 {
			t.Fatal("integer-valued JSON top_k lost")
		}
		if in.KnowledgeBaseIDs != nil {
			if len(in.KnowledgeBaseIDs) != 0 {
				t.Fatal("empty KBs changed")
			}
			return []contracts.Candidate{}, nil
		}
		return []contracts.Candidate{{Text: "verified", Rank: 1, RetrieverID: "private", Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}}}, nil
	})
	for _, empty := range []bool{false, true} {
		body := `{"query":"中文","space_id":"space","top_k":1.0`
		if empty {
			body += `,"kb_ids":[]`
		}
		body += `}`
		r := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
		w := httptest.NewRecorder()
		(api.PartnerSearchHandler{Service: app}).ServeHTTP(w, r)
		var out struct {
			Evidence  []json.RawMessage `json:"evidence"`
			Digest    string            `json:"profile_digest"`
			RequestID string            `json:"request_id"`
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || w.Code != 200 || out.Digest != strings.Repeat("a", 64) || out.RequestID == "" || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Code, w.Body.String())
		}
		expected := 1
		if empty {
			expected = 0
		}
		if len(out.Evidence) != expected {
			t.Fatal("KB list semantics changed")
		}
		for _, b := range captured {
			if b != 0 {
				t.Fatal("credential argument retained")
			}
		}
	}
}

func TestPartnerSearchMapsTypedAuthenticationFailure(t *testing.T) {
	app := application(func(context.Context, knowledge.SearchInput) ([]contracts.Candidate, error) {
		return nil, knowledge.ErrUnauthenticated
	})
	r := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(`{"query":"x","space_id":"space"}`))
	r.Header.Set("Authorization", "Bearer invalid")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	(api.PartnerSearchHandler{Service: app}).ServeHTTP(w, r)
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" || out["code"] != "unauthenticated" || out["retryable"] != false {
		t.Fatal(w.Code, w.Body.String())
	}
}
