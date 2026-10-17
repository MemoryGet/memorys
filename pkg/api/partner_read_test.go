package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

type readApplication func(context.Context, knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error)

func (f readApplication) ReadCurrent(c context.Context, q knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error) {
	return f(c, q)
}

func TestPartnerReadStrictRequestAndUnsupportedCursor(t *testing.T) {
	calls := 0
	app := readApplication(func(context.Context, knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error) {
		calls++
		return contracts.FrozenContentRecord{}, errors.New("private")
	})
	for _, body := range []string{`null`, `{"document_id":"doc","document_id":"other"}`, `{"document_id":"doc","space_id":"forged"}`, `{"document_id":"doc","block_ids":null}`, `{"document_id":"doc","block_ids":["b","b"]}`, `{"document_id":"doc","revision_id":null}`, `{"document_id":"doc","cursor":""}`, `{"document_id":"doc","cursor":"signed-but-unsupported"}`} {
		r := httptest.NewRequest("POST", "/read", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		(api.PartnerReadHandler{Service: app, Profile: application(nil)}).ServeHTTP(w, r)
		expected := 400
		if strings.Contains(body, "signed-but-unsupported") {
			expected = 503
		}
		if w.Code != expected || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 0 {
		t.Fatal("invalid request reached read application")
	}
}

func TestPartnerReadSelectionTraceAndCredentialLifetime(t *testing.T) {
	frozen, err := document.BuildFrozenContent(contracts.Draft{ID: "draft", DocumentID: "doc", State: "draft", RowVersion: 1, Blocks: []contracts.DraftBlock{{ID: "block", DraftID: "draft", RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: "已验证正文", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, selection := range []string{"", `,"block_ids":[]`, `,"block_ids":["block"]`} {
		var secret []byte
		app := readApplication(func(c context.Context, q knowledge.CurrentReadInput) (contracts.FrozenContentRecord, error) {
			secret = q.Credential.Secret
			if q.DocumentID != "doc" || q.RevisionID != "rev" || q.Request.ProfileDigest != contracts.Digest(strings.Repeat("a", 64)) || c.Err() != nil {
				t.Fatal("read binding lost")
			}
			return contracts.FrozenContentRecord{DocumentID: "doc", RevisionID: "rev", Payload: frozen.Payload}, nil
		})
		r := httptest.NewRequest(http.MethodPost, "/read", strings.NewReader(`{"document_id":"doc","revision_id":"rev"`+selection+`}`))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		(api.PartnerReadHandler{Service: app, Profile: application(nil)}).ServeHTTP(w, r)
		var out struct {
			Blocks    []any  `json:"blocks"`
			Citations []any  `json:"citations"`
			Profile   string `json:"profile_digest"`
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || w.Code != 200 || out.Profile != strings.Repeat("a", 64) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		count := 1
		if strings.Contains(selection, "[]") {
			count = 0
		}
		if len(out.Blocks) != count || len(out.Citations) != count {
			t.Fatal("block selection changed")
		}
		for _, b := range secret {
			if b != 0 {
				t.Fatal("credential retained")
			}
		}
	}
}
