package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type blobContentApp struct{}

func (blobContentApp) OpenBlob(context.Context, contracts.Credential, contracts.RequestContext, contracts.ID) (contracts.BlobRef, io.ReadCloser, error) {
	return contracts.BlobRef{ID: "blob-1", Size: 3, MediaType: "text/plain"}, io.NopCloser(strings.NewReader("abc")), nil
}

func TestBlobContentStreamsAuthorizedLogicalBlob(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-os/blobs/blob-1/content", nil)
	r.SetPathValue("blob_id", "blob-1")
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	api.BlobContentHandler{Service: blobContentApp{}}.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "abc" || w.Header().Get("Content-Length") != "3" {
		t.Fatalf("status=%d body=%q headers=%v", w.Code, w.Body.String(), w.Header())
	}
}
