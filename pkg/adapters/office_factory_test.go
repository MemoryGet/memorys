package adapters_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/parser"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type officeConversion struct{}

func (*officeConversion) Convert(context.Context, string, []byte) (parser.MarkdownConversion, error) {
	return parser.MarkdownConversion{}, nil
}
func TestOfficeFactoryRejectsOCRAndUnavailableConverter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var missing *officeConversion
	if err := adapters.OfficeFactory(missing, func(context.Context) error { return nil }).Validate(json.RawMessage(`{"max_bytes":1024}`)); err == nil {
		t.Fatal("typed nil converter accepted")
	}
	f := adapters.OfficeFactory(&officeConversion{}, func(context.Context) error { return nil })
	plan := kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "office", Provider: f.Manifest.ID, Settings: json.RawMessage(`{"max_bytes":1024}`), DependencyBindings: map[string]string{"blob_store": "blob"}}, {ID: "blob", Provider: "test.blob", Settings: json.RawMessage(`{"name":"fixture"}`)}}, Bindings: map[contracts.Capability]string{contracts.Parsing: "office"}}
	created := 0
	var opened []string
	store := storage.LocalBlobStore{Root: t.TempDir(), Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	factories := []kernel.Factory{f, blobFactory(store, &created, &opened)}
	if _, err := kernel.Build(ctx, plan, factories, kernel.Requirement{Capability: contracts.Parsing, Features: []string{"ocr"}}); err == nil {
		t.Fatal("unsupported OCR admitted")
	}
	if err := f.Validate(json.RawMessage(`{"max_bytes":1024,"python":"client-executable"}`)); err == nil {
		t.Fatal("client executable setting accepted")
	}
}

func TestPyPDFFactoryNarrowCapability(t *testing.T) {
	f := adapters.PyPDFFactory(&officeConversion{}, func(context.Context) error { return nil })
	features := f.Manifest.CapabilityFeatures[contracts.Parsing]
	got := map[string]bool{}
	for _, feature := range features {
		got[feature] = true
	}
	if f.Manifest.ID != "platform.pypdf" || !got["format.pdf"] || !got["projection.text"] || got["format.docx"] || got["projection.markdown"] || got["ocr"] {
		t.Fatalf("false capabilities: %+v", features)
	}
	if err := f.Validate(json.RawMessage(`{"script":"untrusted"}`)); err == nil {
		t.Fatal("executable path accepted")
	}
}
