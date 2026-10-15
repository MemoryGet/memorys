package adapters_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/parser"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type contractConverter struct {
	calls   int
	version string
}

func (c *contractConverter) Convert(context.Context, string, []byte) (parser.MarkdownConversion, error) {
	c.calls++
	return parser.MarkdownConversion{Text: "设备维护候选", Version: c.version}, nil
}

// The same standard-port suite runs against both factories and the actual local
// blob store. Converter/current authorization are fixtures, not production IAM.
func TestProjectionParserSharedContract(t *testing.T) {
	for _, name := range []string{"markitdown", "pypdf"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			deadline, _ := ctx.Deadline()
			scope := contracts.AccessScope{ScopeID: "scope", SubjectID: "user", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, Action: "document.upload", ExpiresAt: deadline}
			denyRead := false
			store := storage.LocalBlobStore{Root: t.TempDir(), Authorize: func(_ context.Context, s contracts.AccessScope, _ contracts.BlobRef) error {
				if s.SubjectID != "user" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || s.Action != "document.upload" && s.Action != "document.parse" || denyRead && s.Action == "document.parse" {
					return errors.New("fixture denied")
				}
				return nil
			}}
			raw := []byte("fixture PDF bytes")
			hash := sha256.Sum256(raw)
			digest := contracts.Digest(hex.EncodeToString(hash[:]))
			request := contracts.RequestContext{RequestID: "request", ProfileDigest: digest, Deadline: deadline}
			blob, err := store.Put(ctx, contracts.BlobWriteRequest{Request: request, Scope: scope, Size: int64(len(raw)), ExpectedSHA256: digest, MediaType: "application/pdf", IdempotencyKey: "fixture"}, bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			converter := &contractConverter{version: "markitdown/0.1.3"}
			factory := adapters.OfficeFactory(converter, func(context.Context) error { return nil })
			if name == "pypdf" {
				converter.version = "pypdf/6.19.0"
				factory = adapters.PyPDFFactory(converter, func(context.Context) error { return nil })
			}
			created := 0
			var opened []string
			plan := kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "blob", Provider: "test.blob", Settings: json.RawMessage(`{"name":"fixture"}`)}, {ID: "parser", Provider: factory.Manifest.ID, Settings: json.RawMessage(`{"max_bytes":1024}`), DependencyBindings: map[string]string{"blob_store": "blob"}}}, Bindings: map[contracts.Capability]string{contracts.Parsing: "parser"}}
			runtime, err := kernel.Build(ctx, plan, []kernel.Factory{blobFactory(store, &created, &opened), factory}, kernel.Requirement{Capability: contracts.Parsing, Features: []string{"format.pdf", "source.coarse"}})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close(ctx)
			port, err := kernel.Resolve[contracts.Parser](runtime, contracts.Parsing)
			if err != nil {
				t.Fatal(err)
			}
			scope.Action = "document.parse"
			base := contracts.ParseRequest{Request: request, Scope: scope, Input: blob, DraftID: "draft", Format: "pdf"}
			for _, test := range []struct {
				name   string
				change func(*contracts.ParseRequest)
			}{
				{"missing profile", func(r *contracts.ParseRequest) { r.Request.ProfileDigest = "" }},
				{"invalid profile", func(r *contracts.ParseRequest) { r.Request.ProfileDigest = "wrong" }},
				{"missing request", func(r *contracts.ParseRequest) { r.Request.RequestID = "" }},
				{"missing source", func(r *contracts.ParseRequest) { r.Input.ID = "" }},
				{"missing draft", func(r *contracts.ParseRequest) { r.DraftID = "" }},
				{"wrong digest", func(r *contracts.ParseRequest) { r.Input.SHA256 = contracts.Digest(strings.Repeat("0", 64)) }},
				{"wrong size", func(r *contracts.ParseRequest) { r.Input.Size++ }},
				{"wrong tenant", func(r *contracts.ParseRequest) { r.Scope.SpaceID = "other" }},
				{"wrong action", func(r *contracts.ParseRequest) { r.Scope.Action = "knowledge.search" }},
				{"empty access", func(r *contracts.ParseRequest) { r.Scope.KnowledgeBaseIDs = nil }},
				{"expired scope", func(r *contracts.ParseRequest) { r.Scope.ExpiresAt = time.Now().Add(-time.Second) }},
				{"expired request", func(r *contracts.ParseRequest) { r.Request.Deadline = time.Now().Add(-time.Second) }},
				{"unsupported OCR", func(r *contracts.ParseRequest) { r.Options.EnableOCR = true }},
			} {
				t.Run(test.name, func(t *testing.T) {
					r := base
					test.change(&r)
					before := converter.calls
					result, err := port.Parse(ctx, r)
					if err == nil || len(result.Blocks) != 0 || converter.calls != before {
						t.Fatalf("invalid input reached converter: %+v %v", result, err)
					}
				})
			}
			denyRead = true
			if result, err := port.Parse(ctx, base); err == nil || len(result.Blocks) != 0 || converter.calls != 0 {
				t.Fatal("authorization denial leaked candidate")
			}
			denyRead = false
			result, err := port.Parse(ctx, base)
			if err != nil || len(result.Blocks) != 1 {
				t.Fatal("candidate failed", err)
			}
			anchor := result.Blocks[0].Content.Source
			if anchor.BlobID != blob.ID || anchor.Kind != "original" || anchor.RevisionBlockID != "" || anchor.Locator.Precision != "coarse" || anchor.Locator.Page != nil || anchor.Locator.Characters != nil {
				t.Fatalf("fabricated citation: %+v", anchor)
			}
			again, err := port.Parse(ctx, base)
			if err != nil || again.Blocks[0].ID != result.Blocks[0].ID {
				t.Fatal("unstable candidate identity")
			}
			changed := base
			changed.Request.ProfileDigest = contracts.Digest(strings.Repeat("1", 64))
			other, err := port.Parse(ctx, changed)
			if err != nil || other.Blocks[0].ID == result.Blocks[0].ID {
				t.Fatal("profile change reused candidate identity")
			}
			if err := runtime.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := port.Parse(ctx, base); err == nil {
				t.Fatal("closed port usable")
			}
		})
	}
}
