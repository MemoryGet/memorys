package parser

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type officeBlob struct {
	contracts.BlobStore
	raw []byte
}

func (b officeBlob) Open(context.Context, contracts.BlobReadRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b.raw))), nil
}

type convertedOffice struct{ calls int }

func (c *convertedOffice) Convert(context.Context, string, []byte) (MarkdownConversion, error) {
	c.calls++
	return MarkdownConversion{Text: "中文转换候选", Version: "markitdown/0.1.3"}, nil
}
func TestOfficeCandidateCoarseSourceAndInvalidSourceRejected(t *testing.T) {
	raw := []byte("fixture bytes")
	hash := sha256.Sum256(raw)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	r := contracts.ParseRequest{Format: "docx", DraftID: "draft", Input: contracts.BlobRef{ID: "blob", Size: int64(len(raw)), SHA256: contracts.Digest(fmt.Sprintf("%x", hash))}, Request: contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(fmt.Sprintf("%x", hash))}}
	c := &convertedOffice{}
	p := OfficeParser{Store: officeBlob{raw: raw}, Converter: c}
	result, err := p.Parse(ctx, r)
	if err != nil || len(result.Blocks) != 1 || result.Blocks[0].Content.Source.Locator.Precision != "coarse" || result.Blocks[0].Content.Source.Locator.Characters != nil || len(result.Diagnostics) == 0 {
		t.Fatal("converted text claimed exact source", err)
	}
	r.Input.SHA256 = contracts.Digest(strings.Repeat("0", 64))
	if _, err := p.Parse(ctx, r); err == nil || c.calls != 1 {
		t.Fatal("bad source reached converter")
	}
	r.Options.EnableOCR = true
	if _, err := p.Parse(ctx, r); err == nil || c.calls != 1 {
		t.Fatal("unsupported OCR silently accepted")
	}
}
