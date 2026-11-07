package api

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type draftSourceFixture struct{}

func (draftSourceFixture) ProfileIdentity() (uint64, contracts.Digest, error) {
	return revisionReadFixture{}.ProfileIdentity()
}
func (draftSourceFixture) ReplaceDraftSource(context.Context, contracts.DraftSourceInput, io.Reader) (contracts.DraftParseResult, error) {
	return contracts.DraftParseResult{DocumentID: "document-1", Job: contracts.Job{ID: "parse-job-1", SpaceID: "space-1", State: "queued", Stage: "document.parse", Attempt: 1, RowVersion: 1}}, nil
}

func TestDraftSourceHandlerReturnsQueuedJob(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("expected_version", "2")
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="x.txt"`)
	header.Set("Content-Type", "text/plain")
	part, _ := writer.CreatePart(header)
	_, _ = part.Write([]byte("new source"))
	writer.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-os/drafts/draft-1/source", &body)
	r.SetPathValue("draft_id", "draft-1")
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "source-1")
	w := httptest.NewRecorder()
	(DraftSourceHandler{Service: draftSourceFixture{}}).ServeHTTP(w, r)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"id":"parse-job-1"`) {
		t.Fatalf("source response: %d %s", w.Code, w.Body.String())
	}
}
