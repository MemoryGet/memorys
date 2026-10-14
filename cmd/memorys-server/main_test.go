package main

import (
	"net/http/httptest"
	"testing"
)

func TestBootstrapDoesNotExposeUnconfiguredBusinessCapabilities(t *testing.T) {
	for _, c := range []struct {
		method, path string
		code         int
	}{{"GET", "/health", 200}, {"POST", "/api/documents/upload", 503}, {"GET", "/api/knowledge/search", 503}} {
		w := httptest.NewRecorder()
		newMux().ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.code {
			t.Fatal(c.path, w.Code)
		}
	}
}
