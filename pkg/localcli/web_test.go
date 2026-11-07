package localcli

import (
	"database/sql"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebSecurityBoundary(t *testing.T) {
	s, err := createState(filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	s.m.Ready = true
	assets := t.TempDir()
	if os.WriteFile(filepath.Join(assets, "index.html"), []byte("<html>local ui</html>"), 0600) != nil {
		t.Fatal("fixture")
	}
	h, err := newWebHandler(&sql.DB{}, s, "127.0.0.1:8080", assets)
	if err != nil {
		t.Fatal(err)
	}
	open := httptest.NewRecorder()
	h.ServeHTTP(open, httptest.NewRequest("GET", "http://127.0.0.1:8080/local/", nil))
	if open.Code != 200 || len(open.Result().Cookies()) != 1 || !open.Result().Cookies()[0].HttpOnly || open.Result().Cookies()[0].SameSite != http.SameSiteStrictMode || !strings.Contains(open.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("unsafe bootstrap")
	}
	cookie := open.Result().Cookies()[0]
	for _, test := range []struct {
		name, method, path, host, origin, site, custom string
		cookie                                         bool
	}{
		{"no session", "GET", "/local/api/documents", "127.0.0.1:8080", "", "", "", false},
		{"no session tag list", "GET", "/local/api/tags", "127.0.0.1:8080", "", "", "", false},
		{"no session metrics", "GET", "/local/api/metrics", "127.0.0.1:8080", "", "", "", false},
		{"no session metrics prometheus", "GET", "/local/api/metrics/prometheus", "127.0.0.1:8080", "", "", "", false},
		{"no session task events", "GET", "/local/api/jobs/events", "127.0.0.1:8080", "", "", "", false},
		{"no session feedback list", "GET", "/local/api/feedback", "127.0.0.1:8080", "", "", "", false},
		{"no session evaluation sets", "GET", "/local/api/evaluation-sets", "127.0.0.1:8080", "", "", "", false},
		{"no session feedback export", "GET", "/local/api/feedback/export", "127.0.0.1:8080", "", "", "", false},
		{"no session audit list", "GET", "/local/api/audit", "127.0.0.1:8080", "", "", "", false},
		{"no session export", "GET", "/local/api/export/draft", "127.0.0.1:8080", "", "", "", false},
		{"no session batch import", "POST", "/local/api/import/batch", "127.0.0.1:8080", "", "", "1", false},
		{"no session cited original", "GET", "/local/api/original/document/revision/blob", "127.0.0.1:8080", "", "", "", false},
		{"no session tags", "POST", "/local/api/tags", "127.0.0.1:8080", "", "", "1", false},
		{"no session feedback", "POST", "/local/api/feedback", "127.0.0.1:8080", "", "", "1", false},
		{"no session feedback proposal", "POST", "/local/api/feedback/proposal", "127.0.0.1:8080", "", "", "1", false},
		{"no session feedback resolve", "POST", "/local/api/feedback/resolve", "127.0.0.1:8080", "", "", "1", false},
		{"no session answer stream", "POST", "/local/api/search/stream", "127.0.0.1:8080", "", "", "1", false},
		{"no session publication cancel", "POST", "/local/api/cancel", "127.0.0.1:8080", "", "", "1", false},
		{"wrong host", "GET", "/local/", "attacker.invalid", "", "", "", false},
		{"foreign navigation", "GET", "/local/", "127.0.0.1:8080", "", "cross-site", "", false},
		{"foreign read", "GET", "/local/api/documents", "127.0.0.1:8080", "https://attacker.invalid", "", "", true},
		{"foreign form", "POST", "/local/api/publish", "127.0.0.1:8080", "https://attacker.invalid", "", "1", true},
		{"no origin", "POST", "/local/api/publish", "127.0.0.1:8080", "", "", "1", true},
		{"no custom header", "POST", "/local/api/publish", "127.0.0.1:8080", "http://127.0.0.1:8080", "", "", true},
		{"cross site read", "GET", "/local/api/status", "127.0.0.1:8080", "", "cross-site", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(test.method, "http://127.0.0.1:8080"+test.path, strings.NewReader(`{}`))
			r.Host = test.host
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Sec-Fetch-Site", test.site)
			r.Header.Set("X-Memorys-Local", test.custom)
			if test.cookie {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("guard did not reject before database use: %d", w.Code)
			}
		})
	}
	for _, path := range []string{"/local/private.pem", "/local/assets/private.pem", "/local/assets/manifest.json"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8080"+path, nil))
		if w.Code != 404 {
			t.Fatal("nonasset exposed", path)
		}
	}
	if _, err := parse([]string{"web", "--address", "0.0.0.0:8080"}); err == nil {
		t.Fatal("web nonloopback accepted")
	}
}

func TestDocumentCursorBindsKnowledgeBase(t *testing.T) {
	cursor, err := encodeDocumentCursor(documentCursor{Space: "space", KB: "kb", After: "draft-100"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decodeDocumentCursor(cursor, contracts.ID("space"), contracts.ID("kb")); err != nil || got != "draft-100" {
		t.Fatalf("cursor roundtrip: %q %v", got, err)
	}
	if _, err := decodeDocumentCursor(cursor, contracts.ID("space"), contracts.ID("other")); err == nil {
		t.Fatal("cross-KB cursor accepted")
	}
}

func TestDocumentCursorBindsTagFilter(t *testing.T) {
	cursor, err := encodeDocumentCursor(documentCursor{Space: "space", KB: "kb", After: "draft-100", Tags: []string{"数据库", "运维"}})
	if err != nil {
		t.Fatal(err)
	}
	value, err := decodeDocumentCursorValue(cursor, contracts.ID("space"), contracts.ID("kb"))
	if err != nil || !sameDocumentTags(value.Tags, []string{"数据库", "运维"}) {
		t.Fatalf("tag cursor roundtrip: %#v %v", value, err)
	}
	if sameDocumentTags(value.Tags, []string{"数据库"}) {
		t.Fatal("cursor tag filter can be silently changed")
	}
}

func TestDocumentTagFilterNormalizesRepeatedAndCommaValues(t *testing.T) {
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080/local/api/documents?tag=%E6%95%B0%E6%8D%AE%E5%BA%93&tag=%E8%BF%90%E7%BB%B4%2C%E6%95%B0%E6%8D%AE%E5%BA%93", nil)
	tags, err := documentTagFilter(r)
	if err != nil || !sameDocumentTags(tags, []string{"数据库", "运维"}) {
		t.Fatalf("tag filter: %#v %v", tags, err)
	}
	bad := httptest.NewRequest("GET", "http://127.0.0.1:8080/local/api/documents?tags=%20", nil)
	if _, err := documentTagFilter(bad); err == nil {
		t.Fatal("empty tag filter accepted")
	}
}

func TestWebCitationPreservesDerivedOriginal(t *testing.T) {
	page := 3
	citation := contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", DerivedFrom: []contracts.OriginalAnchor{{BlobID: "blob", Locator: contracts.Locator{Precision: "page", Page: &page}}}}}
	got := webCite(citation)
	if len(got.DerivedFrom) != 1 || got.DerivedFrom[0].BlobID != "blob" || got.DerivedFrom[0].Locator.Page == nil || *got.DerivedFrom[0].Locator.Page != 3 {
		t.Fatalf("derived original was not preserved: %#v", got)
	}
}

func TestWebPublishRequiresExplicitApproval(t *testing.T) {
	h := &webHost{}
	for _, body := range []string{`{"draft_id":"d","expect_hash":"` + strings.Repeat("a", 64) + `","approved":false}`, `{"draft_id":"d","approved":true}`, `{"draft_id":"d","approved":true,"extra":1}`} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/local/api/publish", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if h.publishDocument(r.Context(), httptest.NewRecorder(), r, nil) == nil {
			t.Fatal("unconfirmed publication accepted")
		}
	}
}

func TestWebEditRequiresDraftAndKnownFields(t *testing.T) {
	h := &webHost{}
	for _, body := range []string{`{}`, `{"draft_id":"d"}`, `{"draft_id":"d","expect_hash":"x","text":"x"}`, `{"draft_id":"d","text":"x","extra":1}`} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/local/api/edit", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if h.editDocument(r.Context(), httptest.NewRecorder(), r, nil) == nil {
			t.Fatal("invalid edit request accepted")
		}
	}
}

func TestWebDeleteAndCompareRequireKnownFields(t *testing.T) {
	h := &webHost{}
	for _, test := range []struct {
		name, path, body string
	}{
		{"delete", "/local/api/delete", `{}`},
		{"delete extra", "/local/api/delete", `{"document_id":"d","extra":1}`},
		{"compare", "/local/api/compare", `{}`},
		{"compare extra", "/local/api/compare", `{"draft_id":"d","revision_id":"r","extra":1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://127.0.0.1:8080"+test.path, strings.NewReader(test.body))
			r.Header.Set("Content-Type", "application/json")
			var err error
			if test.path == "/local/api/delete" {
				err = h.deleteDocument(r.Context(), httptest.NewRecorder(), r, nil)
			} else {
				err = h.compareDocument(r.Context(), httptest.NewRecorder(), r, nil)
			}
			if err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestWebSearchPaginationBounds(t *testing.T) {
	h := &webHost{}
	for _, body := range []string{
		`{"query":"x","mode":"keyword","page":-1,"page_size":10}`,
		`{"query":"x","mode":"keyword","page":101,"page_size":10}`,
		`{"query":"x","mode":"keyword","page":1,"page_size":51}`,
		`{"query":"x","mode":"keyword","page":11,"page_size":10}`,
		`{"query":"x","mode":"answer","page":2,"page_size":10}`,
		`{"query":"x","mode":"keyword","source_kind":"table"}`,
		`{"query":"x","mode":"keyword","source_status":"stale"}`,
		`{"query":"x","mode":"keyword","sort":"random"}`,
		`{"query":"x","mode":"keyword","cursor":"not-a-cursor"}`,
		`{"query":"x","mode":"keyword","updated_after":"invalid"}`,
		`{"query":"x","mode":"keyword","updated_after":"2026-02-01T00:00:00Z","updated_before":"2026-01-01T00:00:00Z"}`,
	} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/local/api/search", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if h.search(r.Context(), httptest.NewRecorder(), r, nil) == nil {
			t.Fatalf("invalid pagination accepted: %s", body)
		}
	}
}

func TestWebFeedbackValidation(t *testing.T) {
	h := &webHost{}
	for _, body := range []string{
		`{}`,
		`{"document_id":"d","revision_id":"r","block_id":"b","kind":"unknown","comment":"x"}`,
		`{"document_id":"d","revision_id":"r","block_id":"b","kind":"helpful","comment":"   "}`,
	} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/local/api/feedback", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if h.feedback(r.Context(), httptest.NewRecorder(), r, nil) == nil {
			t.Fatalf("invalid feedback accepted: %s", body)
		}
	}
}
