package contentverify_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type originalProjector func(context.Context, string, []byte) (contentverify.OriginalTextProjection, error)

func (f originalProjector) ProjectOriginalText(c context.Context, m string, b []byte) (contentverify.OriginalTextProjection, error) {
	return f(c, m, b)
}
func originalEvidence(blob contracts.ID) []contracts.Evidence {
	return []contracts.Evidence{{Rank: 1, SourceExcerpt: "原件😀", Citation: contracts.Citation{DocumentID: "document", RevisionID: "revision", BlockID: "block", Source: contracts.SourceAnchor{Kind: "original", BlobID: blob, Locator: contracts.Locator{Precision: "coarse"}}}}}
}

func TestOriginalTextEvidenceUsesActualBytesAndApprovedBlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	q, f, r, store, b, control := approvedOriginalFixture(t, ctx, "text/plain; charset=utf-8")
	e := originalEvidence(b.ID)
	projected := 0
	projector := originalProjector(func(c context.Context, m string, raw []byte) (contentverify.OriginalTextProjection, error) {
		projected++
		if string(raw) != "实际原件😀" {
			t.Fatal("projected bytes were not the actual object")
		}
		return (contentverify.UTF8OriginalText{}).ProjectOriginalText(c, m, raw)
	})
	// Reader callbacks cannot alter the caller batch already bound for checking.
	r.change = func() { e[0].Citation.Source.BlobID = "mutated-caller-batch" }
	if err := contentverify.VerifyOriginalTextEvidence(ctx, r, store, control, projector, q, f, e); err != nil || projected != 1 {
		t.Fatal(err, projected)
	}
}

func TestOriginalTextEvidenceRejectsWrongBindingAndUnsupportedGeometry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, kind := range []string{"document", "revision", "block", "blob", "excerpt", "page", "revision-anchor", "multi-blob"} {
		t.Run(kind, func(t *testing.T) {
			q, f, r, store, b, control := approvedOriginalFixture(t, ctx, "text/plain")
			e := originalEvidence(b.ID)
			switch kind {
			case "document":
				e[0].Citation.DocumentID = "other"
			case "revision":
				e[0].Citation.RevisionID = "other"
			case "block":
				e[0].Citation.BlockID = "absent"
			case "blob":
				e[0].Citation.Source.BlobID = "other"
			case "excerpt":
				e[0].SourceExcerpt = "不存在的文字"
			case "page":
				p := 1
				e[0].Citation.Source.Locator.Page = &p
			case "revision-anchor":
				e[0].Citation.Source.Kind = "revision"
			case "multi-blob":
				extra := e[0]
				extra.Citation.Source.BlobID = "other"
				e = append(e, extra)
			}
			called := false
			projector := originalProjector(func(context.Context, string, []byte) (contentverify.OriginalTextProjection, error) {
				called = true
				return contentverify.OriginalTextProjection{}, nil
			})
			if err := contentverify.VerifyOriginalTextEvidence(ctx, r, store, control, projector, q, f, e); err == nil || called {
				t.Fatal("unbound/unsupported quote reached projection")
			}
		})
	}
}

func TestOriginalTextEvidenceRejectsApprovedEditsAbsentFromOriginal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	q, f, r, store, b, control := approvedOriginalFixture(t, ctx, "text/plain")
	// Editing and approval do not turn new text into an original-file quotation.
	r.record.Payload = []byte(strings.Replace(string(r.record.Payload), "实际原件😀", "人工增加的结论", 1))
	h, err := canonical.Frozen(r.record.Payload)
	if err != nil {
		t.Fatal(err)
	}
	f.ContentHash, f.BlocksHash = contracts.Digest(h.ContentHash), contracts.Digest(h.BlocksHash)
	e := originalEvidence(b.ID)
	e[0].SourceExcerpt = "人工增加的结论"
	if err := contentverify.VerifyOriginalTextEvidence(ctx, r, store, control, contentverify.UTF8OriginalText{}, q, f, e); err == nil {
		t.Fatal("approved edit was misrepresented as an original quote")
	}
}

func TestOriginalTextEvidenceRejectsCorruptBytesAndProjectionRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	q, f, r, store, b, control := approvedOriginalFixture(t, ctx, "text/plain")
	bad := &originalStore{stream: &originalStream{Reader: bytes.NewReader([]byte("wrong"))}}
	called := false
	projector := originalProjector(func(c context.Context, m string, raw []byte) (contentverify.OriginalTextProjection, error) {
		called = true
		return (contentverify.UTF8OriginalText{}).ProjectOriginalText(c, m, raw)
	})
	if err := contentverify.VerifyOriginalTextEvidence(ctx, r, bad, control, projector, q, f, originalEvidence(b.ID)); err == nil || called {
		t.Fatal("corrupt bytes reached text provider")
	}
	projector = func(c context.Context, m string, raw []byte) (contentverify.OriginalTextProjection, error) {
		r.err = errors.New("approval withdrawn during projection")
		return (contentverify.UTF8OriginalText{}).ProjectOriginalText(c, m, raw)
	}
	if err := contentverify.VerifyOriginalTextEvidence(ctx, r, store, control, projector, q, f, originalEvidence(b.ID)); err == nil {
		t.Fatal("withdrawal during projection ignored")
	}
}

func TestUTF8OriginalProjectionRejectsUnsupportedFormatsAndInvalidEncoding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, media := range []string{"application/pdf", "text/plain; charset=gbk", "image/png"} {
		if _, err := (contentverify.UTF8OriginalText{}).ProjectOriginalText(ctx, media, []byte("text")); err == nil {
			t.Fatal("unsupported media accepted", media)
		}
	}
	if _, err := (contentverify.UTF8OriginalText{}).ProjectOriginalText(ctx, "text/plain", []byte{0xff}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}
