package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/parserprocess"
	"github.com/MemoryGet/memorys/pkg/storage"
)

type originalFixtureReader struct{ record contracts.FrozenContentRecord }

func (r originalFixtureReader) ReadFrozenContent(ctx context.Context, q contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error) {
	if ctx.Err() != nil || q.DocumentID != r.record.DocumentID || q.RevisionID != r.record.RevisionID {
		return contracts.FrozenContentRecord{}, fmt.Errorf("fixture frozen read denied")
	}
	out := r.record
	out.Payload = append([]byte(nil), out.Payload...)
	return out, nil
}

type originalFixtureControl struct{}

func (originalFixtureControl) ResolveOriginalRead(ctx context.Context, q contracts.FrozenContentReadRequest, b contracts.BlobRef) (contracts.BlobReadRequest, error) {
	if ctx.Err() != nil || q.Scope.SubjectID != "actor" {
		return contracts.BlobReadRequest{}, fmt.Errorf("fixture original grant denied")
	}
	s := q.Scope
	s.Action = "original.verify"
	s.ScopeID = "fixture-original"
	s.KnowledgeBaseIDs = append([]contracts.ID(nil), s.KnowledgeBaseIDs...)
	return contracts.BlobReadRequest{Request: q.Request, Scope: s, Blob: b}, nil
}

func run() error {
	python := flag.String("python", "", "trusted Python executable")
	worker := flag.String("worker", "", "trusted converter worker")
	source := flag.String("source", "", "fixture source")
	format := flag.String("format", "", "format")
	root := flag.String("root", "", "fixture blob store root")
	provider := flag.String("provider", "platform.markitdown-office", "registered parser provider")
	flag.Parse()
	raw, err := os.ReadFile(*source)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	scope := contracts.AccessScope{ScopeID: "fixture", SubjectID: "actor", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, Action: "document.upload", ExpiresAt: deadline}
	request := contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(hex.EncodeToString(digest[:]))}
	store := storage.LocalBlobStore{Root: *root, Authorize: func(c context.Context, s contracts.AccessScope, _ contracts.BlobRef) error {
		if c.Err() != nil || s.SubjectID != "actor" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return fmt.Errorf("fixture scope denied")
		}
		return nil
	}}
	media := map[string]string{"pdf": "application/pdf", "docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation"}[*format]
	if media == "" {
		return fmt.Errorf("unsupported source format")
	}
	blob, err := store.Put(ctx, contracts.BlobWriteRequest{Request: request, Scope: scope, Size: int64(len(raw)), ExpectedSHA256: contracts.Digest(hex.EncodeToString(digest[:])), MediaType: media, IdempotencyKey: *format}, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	scope.Action = "document.parse"
	converter := parserprocess.MarkItDown{Python: *python, Script: *worker}
	factory := adapters.OfficeFactory(converter, converter.Health)
	if *provider == "platform.pypdf" {
		pdf := parserprocess.PyPDF{Python: *python, Script: *worker}
		factory = adapters.PyPDFFactory(pdf, pdf.Health)
	} else if *provider != "platform.markitdown-office" {
		return fmt.Errorf("unregistered provider")
	}
	blobFactory := kernel.Factory{Manifest: kernel.Manifest{ID: "fixture.blob-store", Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.BlobStorage}, CapabilityFeatures: map[contracts.Capability][]string{contracts.BlobStorage: {"stream.read"}}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
		return &kernel.Instance{Services: map[contracts.Capability]any{contracts.BlobStorage: store}, Health: func(c context.Context) error { return c.Err() }, Close: func(context.Context) error { return nil }}, nil
	}}
	plan := kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "blobs", Provider: "fixture.blob-store", Settings: json.RawMessage(`{}`)}, {ID: "parser", Provider: *provider, Settings: json.RawMessage(`{"max_bytes":8388608}`), DependencyBindings: map[string]string{"blob_store": "blobs"}}}, Bindings: map[contracts.Capability]string{contracts.Parsing: "parser"}}
	runtime, err := kernel.Build(ctx, plan, []kernel.Factory{blobFactory, factory}, kernel.Requirement{Capability: contracts.Parsing, Features: []string{"format." + *format, "source.coarse", "original.text", "original.format." + *format}})
	if err != nil {
		return err
	}
	defer runtime.Close(ctx)
	p, err := kernel.Resolve[contracts.Parser](runtime, contracts.Parsing)
	if err != nil {
		return err
	}
	result, err := p.Parse(ctx, contracts.ParseRequest{Request: request, Scope: scope, DraftID: "draft", Input: blob, Format: *format})
	if err != nil {
		return err
	}
	if len(result.Blocks) != 1 || result.Blocks[0].Content.Text == "" || result.Blocks[0].Content.Source.BlobID != blob.ID || result.Blocks[0].Content.Source.Locator.Precision != "coarse" || result.Blocks[0].Content.Source.Locator.Characters != nil {
		return fmt.Errorf("candidate source contract mismatch")
	}
	candidate, err := document.CandidateFromParse(contracts.ParseRequest{Request: request, Scope: scope, DraftID: "draft", Input: blob, Format: *format}, 1, result)
	if err != nil {
		return err
	}
	draft := contracts.Draft{ID: "draft", DocumentID: "document", RowVersion: 1, State: "draft", Blobs: []contracts.BlobRef{blob}}
	merged, err := document.MergeParsedCandidate(draft, candidate)
	if err != nil || !merged.Applied || merged.Draft.RowVersion != 2 || merged.Draft.Blocks[0].RowVersion != 1 {
		return fmt.Errorf("candidate merge rejected")
	}
	draft.RowVersion = 2
	late, err := document.MergeParsedCandidate(draft, candidate)
	if err != nil || late.Applied || late.Reason != "draft_changed" || len(late.Candidate.Blocks) != 1 {
		return fmt.Errorf("late candidate overwrote newer draft")
	}
	projector, err := kernel.Resolve[contentverify.OriginalTextProjector](runtime, contracts.Parsing)
	if err != nil {
		return err
	}
	frozen, err := document.BuildFrozenContent(merged.Draft)
	if err != nil {
		return err
	}
	searchScope := scope
	searchScope.Action = "knowledge.search"
	read := contracts.FrozenContentReadRequest{Request: request, Scope: searchScope, DocumentID: "document", RevisionID: "revision"}
	reader := originalFixtureReader{record: contracts.FrozenContentRecord{DocumentID: read.DocumentID, RevisionID: read.RevisionID, Payload: frozen.Payload}}
	expected := contracts.FrozenRevisionFact{ID: read.RevisionID, DocumentID: read.DocumentID, ContentHash: frozen.ContentHash, BlocksHash: frozen.BlocksHash, HashFormat: frozen.HashFormat}
	evidence := []contracts.Evidence{{Rank: 1, SourceExcerpt: merged.Draft.Blocks[0].Content.Text, Citation: contracts.Citation{DocumentID: read.DocumentID, RevisionID: read.RevisionID, BlockID: merged.Draft.Blocks[0].ID, Source: merged.Draft.Blocks[0].Content.Source}}}
	if err := contentverify.VerifyOriginalTextEvidence(ctx, reader, store, originalFixtureControl{}, projector, read, expected, evidence); err != nil {
		return fmt.Errorf("actual original quote verification failed")
	}
	invented := append([]contracts.Evidence(nil), evidence...)
	invented[0].SourceExcerpt = "invented-quote-not-in-approved-original-42861"
	if contentverify.VerifyOriginalTextEvidence(ctx, reader, store, originalFixtureControl{}, projector, read, expected, invented) == nil {
		return fmt.Errorf("invented original quote accepted")
	}
	if err := runtime.Close(ctx); err != nil {
		return err
	}
	if _, err := p.Parse(ctx, contracts.ParseRequest{}); err == nil {
		return fmt.Errorf("closed parser reference usable")
	}
	if _, err := projector.ProjectOriginalText(ctx, media, raw); err == nil {
		return fmt.Errorf("closed original projector reference usable")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": true, "production_acceptance": false, "configured_factory": true, "closed_reference_rejected": true, "closed_projector_rejected": true, "original_quote_verified": true, "invented_quote_rejected": true, "candidate_merge": true, "late_candidate_preserved": true, "format": *format, "provider": *provider, "parser_version": result.ParserVersion, "text": result.Blocks[0].Content.Text, "source_sha256": blob.SHA256, "source_precision": result.Blocks[0].Content.Source.Locator.Precision, "scope": "Real blob/Go composite parser/process/converter and original quote; fixture authorization/frozen content and nonpersistent draft merge"})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Office parser probe rejected:", err)
		os.Exit(1)
	}
}
