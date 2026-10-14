package adapters_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type countedBlob struct {
	contracts.BlobStore
	name   string
	opened *[]string
}

func (b countedBlob) Open(ctx context.Context, r contracts.BlobReadRequest) (io.ReadCloser, error) {
	*b.opened = append(*b.opened, b.name)
	return b.BlobStore.Open(ctx, r)
}
func blobFactory(store contracts.BlobStore, created *int, opened *[]string) kernel.Factory {
	return kernel.Factory{Manifest: kernel.Manifest{ID: "test.blob", Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.BlobStorage}, CapabilityFeatures: map[contracts.Capability][]string{contracts.BlobStorage: {"stream.read"}}}, Validate: func(json.RawMessage) error { return nil }, Create: func(_ context.Context, raw json.RawMessage, _ kernel.Lookup) (*kernel.Instance, error) {
		*created++
		var settings struct {
			Name string `json:"name"`
		}
		json.Unmarshal(raw, &settings)
		return &kernel.Instance{Services: map[contracts.Capability]any{contracts.BlobStorage: countedBlob{store, settings.Name, opened}}, Health: func(context.Context) error { return nil }, Close: func(context.Context) error { return nil }}, nil
	}}
}
func config(binding string) kernel.Config {
	return kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "first", Provider: "test.blob", Settings: json.RawMessage(`{"name":"first"}`)}, {ID: "second", Provider: "test.blob", Settings: json.RawMessage(`{"name":"second"}`)}, {ID: "parser", Provider: "platform.markdown", Settings: json.RawMessage(`{"max_bytes":8388608}`), DependencyBindings: map[string]string{"blob_store": binding}}, {ID: "splitter", Provider: "platform.character-splitter", Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.Parsing: "parser", contracts.Splitting: "splitter"}}
}

func TestConfiguredProcessingPortsReadRealBlobAndRejectClosedReferences(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store := storage.LocalBlobStore{Root: t.TempDir(), Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	text := []byte("真实中文😀\n")
	hash := sha256.Sum256(text)
	r := contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute), ProfileDigest: contracts.Digest(fmt.Sprintf("%x", hash))}
	scope := contracts.AccessScope{ScopeID: "scope", SubjectID: "subject", Action: "document.parse", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: r.Deadline}
	blob, err := store.Put(ctx, contracts.BlobWriteRequest{Request: r, Scope: scope, Size: int64(len(text)), ExpectedSHA256: contracts.Digest(fmt.Sprintf("%x", hash)), MediaType: "text/markdown", IdempotencyKey: "key"}, bytes.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range []string{"first", "second"} {
		created := 0
		var opened []string
		runtime, err := kernel.Build(ctx, config(binding), []kernel.Factory{blobFactory(store, &created, &opened), adapters.MarkdownFactory(), adapters.SplitterFactory()}, kernel.Requirement{Capability: contracts.Parsing, ContractVersion: "1.0", Features: []string{"text.preserve", "source.characters"}}, kernel.Requirement{Capability: contracts.Splitting, Features: []string{"offset.unicode"}})
		if err != nil {
			t.Fatal(err)
		}
		p, err := kernel.Resolve[contracts.Parser](runtime, contracts.Parsing)
		if err != nil {
			t.Fatal(err)
		}
		s, err := kernel.Resolve[contracts.Splitter](runtime, contracts.Splitting)
		if err != nil {
			t.Fatal(err)
		}
		parseReq := contracts.ParseRequest{Request: r, Scope: scope, DraftID: "draft", Input: blob, Format: "markdown"}
		parsed, err := p.Parse(ctx, parseReq)
		if err != nil || parsed.Blocks[0].Content.Text != string(text) {
			t.Fatal("real assembled parser failed", err)
		}
		if len(opened) != 1 || opened[0] != binding {
			t.Fatal("parser used wrong dependency instance", opened, binding)
		}
		splitReq := contracts.SplitRequest{Request: r, Scope: scope, RevisionID: "rev", Blocks: []contracts.Block{{ID: parsed.Blocks[0].ID, RevisionID: "rev", Content: parsed.Blocks[0].Content}}, Policy: contracts.ChunkPolicy{Strategy: "characters", MaxCodepoints: 3}}
		chunks, err := s.Split(ctx, splitReq)
		if err != nil || len(chunks) < 2 {
			t.Fatal("assembled splitter failed", err)
		}
		if err := runtime.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Parse(ctx, parseReq); err == nil {
			t.Fatal("closed parser reference usable")
		}
		if _, err := s.Split(ctx, splitReq); err == nil {
			t.Fatal("closed splitter reference usable")
		}
	}
}
func TestProcessingConfigurationFailsBeforeCreatingDependencies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store := storage.LocalBlobStore{Root: t.TempDir()}
	for _, raw := range []string{`{}`, `{"max_bytes":0}`, `{"max_bytes":8388609}`, `{"max_bytes":1,"max_bytes":2}`, `{"max_bytes":1,"unexpected":true}`, `{"max_bytes":1.0}`} {
		c := config("first")
		c.Modules[2].Settings = json.RawMessage(raw)
		created := 0
		var opened []string
		if runtime, err := kernel.Build(ctx, c, []kernel.Factory{blobFactory(store, &created, &opened), adapters.MarkdownFactory(), adapters.SplitterFactory()}, kernel.Requirement{Capability: contracts.Parsing}); err == nil || runtime != nil || created != 0 {
			t.Fatal("bad configuration created resources", raw, created)
		}
	}
	created := 0
	var opened []string
	b := blobFactory(store, &created, &opened)
	b.Manifest.CapabilityFeatures = nil
	if _, err := kernel.Build(ctx, config("first"), []kernel.Factory{b, adapters.MarkdownFactory(), adapters.SplitterFactory()}, kernel.Requirement{Capability: contracts.Parsing}); err == nil || created != 0 {
		t.Fatal("missing required stream capability assembled")
	}
}
