package contentverify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type OriginalTextProjection struct{ Text, Version string }

// OriginalTextProjector must derive text from the supplied verified bytes.
// PDF/Office/OCR implementations require separate capability/quality admission.
type OriginalTextProjector interface {
	ProjectOriginalText(context.Context, string, []byte) (OriginalTextProjection, error)
}

type UTF8OriginalText struct{}

func (UTF8OriginalText) ProjectOriginalText(ctx context.Context, media string, raw []byte) (OriginalTextProjection, error) {
	kind, params, err := mime.ParseMediaType(media)
	if ctx == nil || ctx.Err() != nil || err != nil || (kind != "text/plain" && kind != "text/markdown") || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) || len(raw) > 8<<20 || !utf8.Valid(raw) {
		return OriginalTextProjection{}, ErrUnsupportedEvidence
	}
	return OriginalTextProjection{Text: string(raw), Version: "utf8-exact/1"}, nil
}

// VerifyOriginalTextEvidence verifies one file-level original/coarse batch.
// Each quote must occur both in its exact approved text block (whose original
// anchor must match) and in a projection of actual hash-verified object bytes.
// It never retags original citations as revision citations or verifies geometry.
func VerifyOriginalTextEvidence(ctx context.Context, reader contracts.FrozenContentReader, store contracts.BlobStore, control OriginalReadControl, projector OriginalTextProjector, request contracts.FrozenContentReadRequest, expected contracts.FrozenRevisionFact, evidence []contracts.Evidence) error {
	if ctx == nil || store == nil || control == nil || projector == nil || len(evidence) == 0 || len(evidence) > 1000 || request.Request.Deadline.IsZero() || request.Scope.ExpiresAt.IsZero() {
		return ErrContent
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrContent
	}
	fixed := append([]contracts.Evidence(nil), evidence...)
	var blobID contracts.ID
	budget := 4 << 20
	for _, e := range fixed {
		a, l := e.Citation.Source, e.Citation.Source.Locator
		if e.Citation.DocumentID != expected.DocumentID || e.Citation.RevisionID != expected.ID || e.Citation.BlockID == "" || e.Rank < 1 || e.SourceExcerpt == "" || !utf8.ValidString(e.SourceExcerpt) || len(e.SourceExcerpt) > budget {
			return ErrContent
		}
		budget -= len(e.SourceExcerpt)
		if a.Kind != "original" || a.BlobID == "" || a.RevisionBlockID != "" || len(a.DerivedFrom) != 0 || l.Precision != "coarse" || l.Characters != nil || l.Page != nil || l.Slide != nil || l.Worksheet != "" || l.CellRange != "" || l.ParagraphPath != nil || l.Box != nil {
			return ErrUnsupportedEvidence
		}
		if blobID != "" && blobID != a.BlobID {
			return ErrUnsupportedEvidence
		}
		blobID = a.BlobID
	}
	request.Scope.KnowledgeBaseIDs = append([]contracts.ID(nil), request.Scope.KnowledgeBaseIDs...)
	ctx, cancel := context.WithDeadline(ctx, request.Request.Deadline)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, request.Scope.ExpiresAt)
	defer expire()
	payload, err := readVerifiedFrozen(ctx, reader, request, expected)
	if err != nil {
		return err
	}
	var content struct {
		Blocks []struct {
			ID     contracts.ID    `json:"id"`
			Kind   string          `json:"kind"`
			Text   string          `json:"text"`
			Source json.RawMessage `json:"source_anchor"`
		} `json:"blocks"`
	}
	if json.Unmarshal(payload, &content) != nil {
		return ErrContent
	}
	byID := map[contracts.ID]int{}
	for i, b := range content.Blocks {
		if b.ID == "" {
			return ErrContent
		}
		if _, duplicate := byID[b.ID]; duplicate {
			return ErrContent
		}
		byID[b.ID] = i
	}
	anchor, _ := json.Marshal(map[string]any{"kind": "original", "blob_id": blobID, "locator": map[string]any{"precision": "coarse"}})
	canonicalAnchor, _ := canonical.JSON(anchor)
	for _, e := range fixed {
		i, ok := byID[e.Citation.BlockID]
		if !ok {
			return ErrContent
		}
		b := content.Blocks[i]
		source, err := canonical.JSON(b.Source)
		if b.Kind != "text" || err != nil || !bytes.Equal(source, canonicalAnchor) || !strings.Contains(b.Text, e.SourceExcerpt) {
			return ErrContent
		}
	}
	captured := &captureOriginalStore{BlobStore: store, blobID: blobID}
	tracked := &trackOriginalControl{inner: control}
	if err := VerifyApprovedOriginal(ctx, reader, captured, tracked, request, expected, blobID); err != nil {
		return err
	}
	projectionCtx, stopProjection := context.WithDeadline(ctx, tracked.deadline)
	defer stopProjection()
	if projectionCtx.Err() != nil {
		return ErrContent
	}
	projection, err := projector.ProjectOriginalText(projectionCtx, captured.media, append([]byte(nil), captured.data...))
	if err != nil || projectionCtx.Err() != nil || projection.Version == "" || len(projection.Version) > 200 || !utf8.ValidString(projection.Version) || len(projection.Text) > 4<<20 || !utf8.ValidString(projection.Text) {
		return ErrContent
	}
	for _, e := range fixed {
		if !strings.Contains(projection.Text, e.SourceExcerpt) {
			return ErrContent
		}
	}
	// Projection may be remote/slow: reauthorize and re-read approval afterwards.
	// This independent pass is deliberately not replaced by a prior success flag.
	if err := VerifyApprovedOriginal(ctx, reader, store, control, request, expected, blobID); err != nil {
		return err
	}
	return ctx.Err()
}

type trackOriginalControl struct {
	inner    OriginalReadControl
	deadline time.Time
}

func (t *trackOriginalControl) ResolveOriginalRead(c context.Context, q contracts.FrozenContentReadRequest, b contracts.BlobRef) (contracts.BlobReadRequest, error) {
	r, err := t.inner.ResolveOriginalRead(c, q, b)
	if err == nil {
		t.deadline = r.Request.Deadline
	}
	return r, err
}

// Capture only bytes from the exact stream checked by VerifyBlobBytes, avoiding
// a hash/read-to-project race. No unverified data is passed to the projector.
type captureOriginalStore struct {
	contracts.BlobStore
	blobID contracts.ID
	media  string
	data   []byte
}

func (s *captureOriginalStore) Open(ctx context.Context, q contracts.BlobReadRequest) (io.ReadCloser, error) {
	if s.BlobStore == nil || q.Blob.ID != s.blobID || q.Blob.Size > 8<<20 {
		return nil, ErrContent
	}
	r, err := s.BlobStore.Open(ctx, q)
	if r == nil {
		return nil, err
	}
	s.media = q.Blob.MediaType
	return &captureOriginalStream{ReadCloser: r, store: s}, err
}

type captureOriginalStream struct {
	io.ReadCloser
	store *captureOriginalStore
}

func (s *captureOriginalStream) Read(p []byte) (int, error) {
	n, err := s.ReadCloser.Read(p)
	if n < 0 || n > len(p) {
		return 0, ErrContent
	}
	if len(s.store.data)+n > 8<<20 {
		return n, ErrContent
	}
	s.store.data = append(s.store.data, p[:n]...)
	return n, err
}
