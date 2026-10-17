package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBootstrapDoesNotExposeUnconfiguredBusinessCapabilities(t *testing.T) {
	for _, c := range []struct {
		method, path string
		code         int
	}{{"GET", "/health", 200}, {"POST", "/api/documents/upload", 503}, {"GET", "/api/knowledge/search", 503}, {"POST", "/api/v1/knowledge-os/read", 503}, {"POST", "/mcp", 503}} {
		w := httptest.NewRecorder()
		newMux().ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.code {
			t.Fatal(c.path, w.Code)
		}
	}
}

func TestPartnerSearchRouteFailsClosedWithoutBootstrap(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/search", nil)
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, r)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"capability_missing"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
