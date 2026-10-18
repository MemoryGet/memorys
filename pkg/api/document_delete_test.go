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
	"github.com/MemoryGet/memorys/pkg/document"
)

type deleteApp func(context.Context, document.AuthenticatedDeleteInput) (contracts.DeleteDocumentResult, error)

func (f deleteApp) DeleteDocument(c context.Context, q document.AuthenticatedDeleteInput) (contracts.DeleteDocumentResult, error) {
	return f(c, q)
}

type deleteProfile struct{}

func (deleteProfile) ProfileIdentity() (uint64, contracts.Digest, error) {
	return 1, contracts.Digest(strings.Repeat("a", 64)), nil
}

func TestDocumentDeleteStrictInputAndAcceptedProjection(t *testing.T) {
	calls := 0
	var held []byte
	service := deleteApp(func(_ context.Context, q document.AuthenticatedDeleteInput) (contracts.DeleteDocumentResult, error) {
		calls++
		held = q.Credential.Secret
		if q.DocumentID != "doc" || q.ExpectedDocumentVersion != 9007199254740993 || q.IdempotencyKey != "key" || q.Request.ProfileDigest != contracts.Digest(strings.Repeat("a", 64)) {
			t.Fatal("deletion input lost exact fields")
		}
		return contracts.DeleteDocumentResult{DocumentID: "doc", DocumentVersion: q.ExpectedDocumentVersion + 1, PublicationEpoch: 3, JobID: "real-job"}, nil
	})
	mux := http.NewServeMux()
	mux.Handle("/documents/{id}", api.DocumentDeleteHandler{Service: service, Profile: deleteProfile{}})
	cases := []struct {
		query, body string
		duplicate   bool
		want        int
	}{
		{"expected_document_version=9007199254740993", "", false, 202},
		{"expected_document_version=0", "", false, 400},
		{"expected_document_version=18446744073709551615", "", false, 400},
		{"expected_document_version=1e3", "", false, 400},
		{"expected_document_version=1&expected_document_version=2", "", false, 400},
		{"expected_document_version=1&space_id=injected", "", false, 400},
		{"expected_document_version=1", "{\"scope\":{}}", false, 400},
		{"expected_document_version=1", "", true, 400},
		{"expected_document_version=%zz", "", false, 400},
	}
	for _, test := range cases {
		r := httptest.NewRequest("DELETE", "/documents/doc?"+test.query, strings.NewReader(test.body))
		r.Header.Set("Authorization", "Bearer token")
		r.Header.Set("Idempotency-Key", "key")
		if test.duplicate {
			r.Header.Add("Idempotency-Key", "other")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatal(test.query, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
		if w.Code == 202 {
			var value map[string]any
			if json.Unmarshal(w.Body.Bytes(), &value) != nil || len(value) != 3 || value["job_id"] != "real-job" {
				t.Fatal("wrong DeleteAccepted projection")
			}
		}
	}
	if calls != 1 || string(held) != strings.Repeat("\x00", 5) {
		t.Fatal("invalid input reached app or credential not cleared")
	}
}
