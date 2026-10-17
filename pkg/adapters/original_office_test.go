package adapters_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/parser"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type originalOfficeConverter struct {
	calls  int
	format string
	result parser.MarkdownConversion
	err    error
}

func (c *originalOfficeConverter) Convert(_ context.Context, f string, raw []byte) (parser.MarkdownConversion, error) {
	c.calls++
	c.format = f
	if len(raw) > 0 {
		raw[0] = '!'
	}
	return c.result, c.err
}
func originalOfficeRuntime(t *testing.T, ctx context.Context, converter *originalOfficeConverter, pdf bool, limit int) (*kernel.Runtime, contentverify.OriginalTextProjector) {
	t.Helper()
	factory := adapters.OfficeFactory(converter, func(context.Context) error { return nil })
	if pdf {
		factory = adapters.PyPDFFactory(converter, func(context.Context) error { return nil })
	}
	var created int
	var opened []string
	store := storage.LocalBlobStore{Root: t.TempDir(), Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	settings, _ := json.Marshal(map[string]int{"max_bytes": limit})
	plan := kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "office", Provider: factory.Manifest.ID, Settings: settings, DependencyBindings: map[string]string{"blob_store": "blobs"}}, {ID: "blobs", Provider: "test.blob", Settings: json.RawMessage(`{"name":"fixture"}`)}}, Bindings: map[contracts.Capability]string{contracts.Parsing: "office"}}
	runtime, err := kernel.Build(ctx, plan, []kernel.Factory{factory, blobFactory(store, &created, &opened)}, kernel.Requirement{Capability: contracts.Parsing, Features: []string{"original.text", "original.format.pdf"}})
	if err != nil {
		t.Fatal(err)
	}
	projector, err := kernel.Resolve[contentverify.OriginalTextProjector](runtime, contracts.Parsing)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, projector
}
func TestOriginalOfficeProjectionUsesConfiguredConverterAndIndependentBytes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := &originalOfficeConverter{result: parser.MarkdownConversion{Text: "真实投影", Version: "fixture/1"}}
	runtime, p := originalOfficeRuntime(t, ctx, c, false, 1024)
	defer runtime.Close(ctx)
	raw := []byte("source")
	got, err := p.ProjectOriginalText(ctx, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", raw)
	if err != nil || got.Text != "真实投影" || got.Version != "fixture/1" || c.format != "docx" || string(raw) != "source" {
		t.Fatal("conversion routing or ownership failed", err)
	}
	got, err = p.ProjectOriginalText(ctx, "text/plain; charset=utf-8", []byte("UTF8正文"))
	if err != nil || got.Text != "UTF8正文" || c.calls != 1 {
		t.Fatal("literal text unexpectedly converted", err)
	}
}
func TestOriginalPDFProjectionRejectsOtherFormatsAndClosedReference(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := &originalOfficeConverter{result: parser.MarkdownConversion{Text: "PDF", Version: "fixture/1"}}
	runtime, p := originalOfficeRuntime(t, ctx, c, true, 1024)
	if _, err := p.ProjectOriginalText(ctx, "application/pdf", []byte("pdf")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ProjectOriginalText(ctx, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("docx")); !errors.Is(err, contentverify.ErrUnsupportedEvidence) || c.calls != 1 {
		t.Fatal("PDF provider claimed Office projection")
	}
	if err := runtime.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ProjectOriginalText(ctx, "application/pdf", []byte("pdf")); err == nil || c.calls != 1 {
		t.Fatal("closed original projector called converter")
	}
}
func TestOriginalOfficeProjectionEnforcesBudgetAndOutputChecks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := &originalOfficeConverter{result: parser.MarkdownConversion{Text: "valid", Version: "fixture/1"}}
	runtime, p := originalOfficeRuntime(t, ctx, c, false, 4)
	defer runtime.Close(ctx)
	if _, err := p.ProjectOriginalText(ctx, "application/pdf", []byte("12345")); err == nil || c.calls != 0 {
		t.Fatal("oversize source reached converter")
	}
	if _, err := p.ProjectOriginalText(context.Background(), "application/pdf", []byte("pdf")); err == nil || c.calls != 0 {
		t.Fatal("unbounded projection admitted")
	}
	c.result.Text = string([]byte{0xff})
	if _, err := p.ProjectOriginalText(ctx, "application/pdf", []byte("pdf")); err == nil {
		t.Fatal("invalid UTF8 output accepted")
	}
	c.result = parser.MarkdownConversion{Text: "valid"}
	if _, err := p.ProjectOriginalText(ctx, "application/pdf", []byte("pdf")); err == nil {
		t.Fatal("missing converter version accepted")
	}
}
