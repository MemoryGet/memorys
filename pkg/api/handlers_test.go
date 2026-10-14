package api_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeStore struct{ writes int }

func (s *fakeStore) Put(context.Context, contracts.BlobWriteRequest, io.Reader) (contracts.BlobRef, error) {
	s.writes++
	return contracts.BlobRef{}, nil
}
func (*fakeStore) Open(context.Context, contracts.BlobReadRequest) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}
func (*fakeStore) Delete(context.Context, contracts.BlobReadRequest) error { return nil }
func TestUploadRequiresAuthorizationAndRejectsOversizeBeforeStorage(t *testing.T) {
	s := &fakeStore{}
	h := api.UploadHandler{Service: ingestion.UploadService{Store: s}}
	w := httptest.NewRecorder()
	h.Handle(w, httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("untrusted")))
	if w.Code != 503 || s.writes != 0 {
		t.Fatal("unconfigured upload allowed")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "test.md")
	file.Write([]byte(strings.Repeat("x", 512)))
	form.Close()
	h.MaxBodyBytes = 128
	h.Prepare = func(*http.Request) (contracts.BlobWriteRequest, error) {
		return contracts.BlobWriteRequest{Request: contracts.RequestContext{Deadline: time.Now().Add(time.Minute)}, Size: 512}, nil
	}
	r := httptest.NewRequest(http.MethodPost, "/upload", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w = httptest.NewRecorder()
	h.Handle(w, r)
	if w.Code != 413 || s.writes != 0 {
		t.Fatal("oversized body reached store", w.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(body.Bytes()))
	r.ContentLength = -1
	r.Header.Set("Content-Type", form.FormDataContentType())
	w = httptest.NewRecorder()
	h.Handle(w, r)
	if w.Code != 413 || s.writes != 0 {
		t.Fatal("chunked oversized upload accepted", w.Code)
	}
}

type fakeRetriever struct{ calls int }

func (r *fakeRetriever) Search(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	r.calls++
	return []contracts.Candidate{{Text: "protected-source", Rank: 1, Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}}}, nil
}
func TestSearchRequiresPreparationAndCurrentOutputVerification(t *testing.T) {
	r := &fakeRetriever{}
	h := api.SearchHandler{Retriever: r}
	w := httptest.NewRecorder()
	h.Handle(w, httptest.NewRequest(http.MethodGet, "/search?q=x", nil))
	if w.Code != 503 || r.calls != 0 {
		t.Fatal("unauthenticated search reached index")
	}
	h.Prepare = func(*http.Request) (contracts.RetrieveRequest, error) {
		return contracts.RetrieveRequest{Request: contracts.RequestContext{Deadline: time.Now().Add(time.Minute)}}, nil
	}
	h.Verify = func(context.Context, contracts.RetrieveRequest, []contracts.Candidate) error {
		return errors.New("revoked private policy")
	}
	w = httptest.NewRecorder()
	h.Handle(w, httptest.NewRequest(http.MethodGet, "/search", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "protected-source") || strings.Contains(w.Body.String(), "private policy") {
		t.Fatal("unverified source/error leaked")
	}
	h.Verify = func(context.Context, contracts.RetrieveRequest, []contracts.Candidate) error { return nil }
	w = httptest.NewRecorder()
	h.Handle(w, httptest.NewRequest(http.MethodGet, "/search", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "source_anchor") || strings.Contains(w.Body.String(), "RetrieverID") {
		t.Fatal("citation missing/private fields exposed", w.Body.String())
	}
}
