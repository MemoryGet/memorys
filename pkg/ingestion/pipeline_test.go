package ingestion_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/chunk"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"github.com/MemoryGet/memorys/pkg/parser"
	"github.com/MemoryGet/memorys/pkg/storage"
)

func TestRealFileParseDraftAndDeterministicUnicodeChunks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store := storage.LocalBlobStore{Root: t.TempDir(), Authorize: func(context.Context, contracts.AccessScope, contracts.BlobRef) error { return nil }}
	text := "  中文😀\r\n\r\n正文  "
	data := []byte(text)
	hash := sha256.Sum256(data)
	request := contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute), ProfileDigest: contracts.Digest(fmt.Sprintf("%x", hash))}
	scope := contracts.AccessScope{ScopeID: "scope", SubjectID: "user", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: request.Deadline, Action: "document.parse"}
	upload := ingestion.UploadService{Store: store}
	blob, err := upload.Upload(ctx, contracts.BlobWriteRequest{Request: request, Scope: scope, ExpectedSHA256: contracts.Digest(fmt.Sprintf("%x", hash)), Size: int64(len(data)), MediaType: "text/markdown", IdempotencyKey: "key"}, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	p := ingestion.Pipeline{Parser: parser.MarkdownParser{Store: store}}
	result, err := p.Process(ctx, contracts.ParseRequest{Request: request, Scope: scope, DraftID: "draft", Input: blob, Format: "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Blocks) != 1 || result.Blocks[0].Content.Text != text || result.Blocks[0].Content.Source.BlobID != blob.ID || result.Blocks[0].RowVersion != 0 {
		t.Fatal("file content or draft ownership lost")
	}
	candidate, err := p.ProcessCandidate(ctx, contracts.ParseRequest{Request: request, Scope: scope, DraftID: "draft", Input: blob, Format: "markdown"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	merge, err := document.MergeParsedCandidate(contracts.Draft{ID: "draft", DocumentID: "document", State: "draft", RowVersion: 1, Blobs: []contracts.BlobRef{blob}}, candidate)
	if err != nil || !merge.Applied || merge.Draft.Blocks[0].Content.Text != text {
		t.Fatal("real file did not produce editable candidate", err)
	}
	if late, err := document.MergeParsedCandidate(merge.Draft, candidate); err != nil || late.Applied || late.Reason != "draft_changed" {
		t.Fatal("late parser could overwrite new draft", err)
	}
	service := ingestion.CandidateService{Parser: p.Parser, ProfileDigest: request.ProfileDigest, Authorize: func(_ context.Context, c ingestion.DraftParseCommand) error {
		if c.Principal.SubjectID != "user" || c.Input != blob || c.DocumentID != "document" {
			return fmt.Errorf("fixture resource denied")
		}
		return nil
	}, Repository: commitCandidateFunc(func(_ context.Context, c document.CandidateCommand) (document.CandidateReceipt, error) {
		if len(c.Candidate.Blocks) != 1 || c.Candidate.Blocks[0].Content.Text != text || c.Candidate.Input != blob {
			t.Fatal("application lost original candidate")
		}
		return document.CandidateReceipt{CandidateID: c.CandidateID, DraftID: c.Candidate.DraftID, DocumentID: c.DocumentID, Applied: true, DraftVersion: 2}, nil
	})}
	prepared, err := service.Prepare(ctx, ingestion.DraftParseCommand{Request: request, Scope: scope, Principal: contracts.Principal{SubjectID: "user", SubjectType: "user", Issuer: "fixture"}, DocumentID: "document", DraftID: "draft", CandidateID: "application-candidate", ExpectedVersion: 1, Input: blob, Format: "markdown"})
	if err != nil {
		t.Fatal("real-file application preparation failed", err)
	}
	if receipt, err := service.Commit(ctx, prepared); err != nil || !receipt.Applied {
		t.Fatal("application commit fixture failed", err)
	}
	req := contracts.SplitRequest{Request: request, Scope: scope, RevisionID: "rev-a", Policy: contracts.ChunkPolicy{Strategy: "characters", MaxCodepoints: 4, OverlapCodepoints: 1}, Blocks: []contracts.Block{{ID: result.Blocks[0].ID, RevisionID: "rev-a", Content: result.Blocks[0].Content}}}
	splitter := chunk.SimpleSplitter{}
	first, err := splitter.Split(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := splitter.Split(ctx, req)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("nondeterministic chunks")
	}
	for _, c := range first {
		span := c.Spans[0].Characters
		if string([]rune(text)[span.Start:span.End]) != c.Text {
			t.Fatal("Unicode offset mismatch")
		}
	}
	req.RevisionID = "rev-b"
	req.Blocks[0].RevisionID = "rev-b"
	other, err := splitter.Split(ctx, req)
	if err != nil || first[0].ID == other[0].ID {
		t.Fatal("revision chunk collision")
	}
	bad := contracts.ParseRequest{Request: request, Scope: scope, DraftID: "draft", Input: blob, Format: "pdf"}
	if _, err := p.Process(ctx, bad); err == nil {
		t.Fatal("unsupported format accepted")
	}
	bad.Format = "markdown"
	bad.Input.SHA256 = contracts.Digest(fmt.Sprintf("%064d", 0))
	if _, err := p.Process(ctx, bad); err == nil {
		t.Fatal("wrong source checksum accepted")
	}
}
