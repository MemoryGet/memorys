package contentverify_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

func textEvidence() []contracts.Evidence {
	return []contracts.Evidence{{Rank: 1, SourceExcerpt: "中文😀", Citation: contracts.Citation{DocumentID: "document", RevisionID: "revision", BlockID: "block-1", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block-1", Locator: contracts.Locator{Precision: "exact", Characters: &contracts.CharacterRange{Start: 0, End: 3}}}}}}
}

func TestTextEvidenceUsesUnicodeRangesAndCoarseContainment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, f, r := fixture(t)
	e := textEvidence()
	r.change = func() { e[0].Citation.Source.Locator.Characters.End = 4 }
	if err := contentverify.VerifyTextEvidence(ctx, r, req, f, e); err != nil {
		t.Fatal("Unicode range or snapshot isolation failed", err)
	}
	e = textEvidence()
	r.change = nil
	e[0].Citation.Source.Locator = contracts.Locator{Precision: "coarse"}
	if err := contentverify.VerifyTextEvidence(ctx, r, req, f, e); err != nil {
		t.Fatal(err)
	}
}

func TestTextEvidenceRejectsBoundsForeignVersionsAndInventedExcerpts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	changes := map[string]func([]contracts.Evidence){
		"UTF16 endpoint":   func(e []contracts.Evidence) { e[0].Citation.Source.Locator.Characters.End = 4 },
		"negative start":   func(e []contracts.Evidence) { e[0].Citation.Source.Locator.Characters.Start = -1 },
		"empty range":      func(e []contracts.Evidence) { e[0].Citation.Source.Locator.Characters.End = 0 },
		"out of bounds":    func(e []contracts.Evidence) { e[0].Citation.Source.Locator.Characters.End = 10000 },
		"wrong excerpt":    func(e []contracts.Evidence) { e[0].SourceExcerpt = "模型新增事实" },
		"empty excerpt":    func(e []contracts.Evidence) { e[0].SourceExcerpt = "" },
		"foreign revision": func(e []contracts.Evidence) { e[0].Citation.RevisionID = "other" },
		"foreign document": func(e []contracts.Evidence) { e[0].Citation.DocumentID = "other" },
		"missing block": func(e []contracts.Evidence) {
			e[0].Citation.BlockID = "absent"
			e[0].Citation.Source.RevisionBlockID = "absent"
		},
		"coarse invented text": func(e []contracts.Evidence) {
			e[0].Citation.Source.Locator = contracts.Locator{Precision: "coarse"}
			e[0].SourceExcerpt = "不存在"
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			req, f, r := fixture(t)
			e := textEvidence()
			change(e)
			if contentverify.VerifyTextEvidence(ctx, r, req, f, e) != contentverify.ErrContent {
				t.Fatal("unverified text accepted")
			}
		})
	}
}

func TestTextEvidenceRejectsUnsupportedLocationsMediaAndUnboundedCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, kind := range []string{"original", "page", "derived", "unknown precision", "exact without range", "coarse with range", "table"} {
		t.Run(kind, func(t *testing.T) {
			req, f, r := fixture(t)
			e := textEvidence()
			switch kind {
			case "original":
				e[0].Citation.Source.Kind = "original"
			case "page":
				page := 1
				e[0].Citation.Source.Locator.Page = &page
			case "derived":
				e[0].Citation.Source.DerivedFrom = []contracts.OriginalAnchor{{BlobID: "original"}}
			case "unknown precision":
				e[0].Citation.Source.Locator.Precision = "vendor"
			case "exact without range":
				e[0].Citation.Source.Locator.Characters = nil
			case "coarse with range":
				e[0].Citation.Source.Locator.Precision = "coarse"
			case "table":
				var root map[string]any
				if json.Unmarshal(r.record.Payload, &root) != nil {
					t.Fatal("fixture")
				}
				root["blocks"].([]any)[0].(map[string]any)["kind"] = "table"
				r.record.Payload, _ = json.Marshal(root)
				hashes, err := canonical.Frozen(r.record.Payload)
				if err != nil {
					t.Fatal(err)
				}
				f.ContentHash, f.BlocksHash = contracts.Digest(hashes.ContentHash), contracts.Digest(hashes.BlocksHash)
			}
			if contentverify.VerifyTextEvidence(ctx, r, req, f, e) != contentverify.ErrUnsupportedEvidence {
				t.Fatal("unsupported evidence silently accepted")
			}
		})
	}
	req, f, r := fixture(t)
	if contentverify.VerifyTextEvidence(context.Background(), r, req, f, textEvidence()) != contentverify.ErrContent || r.calls != 0 {
		t.Fatal("unbounded call read content")
	}
}
