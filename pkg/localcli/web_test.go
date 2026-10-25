package localcli

import (
	"database/sql"
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
