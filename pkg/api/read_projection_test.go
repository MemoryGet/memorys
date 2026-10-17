package api

import (
	"encoding/json"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func TestReadProjectionPreservesOrderContentAndSelection(t *testing.T) {
	d := contracts.Draft{ID: "draft", DocumentID: "doc", RowVersion: 1, State: "draft"}
	for i, id := range []contracts.ID{"one", "two"} {
		d.Blocks = append(d.Blocks, contracts.DraftBlock{ID: id, DraftID: "draft", RowVersion: 1, Order: i, Content: contracts.Content{Kind: contracts.TextBlock, Text: "中文正文 ", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: id, Locator: contracts.Locator{Precision: "coarse"}}}})
	}
	frozen, err := document.BuildFrozenContent(d)
	if err != nil {
		t.Fatal(err)
	}
	r := contracts.FrozenContentRecord{DocumentID: "doc", RevisionID: "rev", Payload: frozen.Payload}
	for _, tc := range []struct {
		ids   []contracts.ID
		count int
		bad   bool
	}{{nil, 2, false}, {[]contracts.ID{}, 0, false}, {[]contracts.ID{"two", "one"}, 2, false}, {[]contracts.ID{"two"}, 1, false}, {[]contracts.ID{"missing"}, 0, true}, {[]contracts.ID{"one", "one"}, 0, true}} {
		out, err := ProjectReadContent(r, tc.ids)
		if tc.bad {
			if err != ErrReadProjection || out != nil {
				t.Fatal("invalid selection accepted")
			}
			continue
		}
		if err != nil {
			t.Fatalf("ids=%v payload=%s err=%v", tc.ids, r.Payload, err)
		}
		blocks := out["blocks"].([]any)
		citations := out["citations"].([]any)
		if len(blocks) != tc.count || len(citations) != tc.count {
			t.Fatal("selection differs")
		}
		for _, item := range blocks {
			b := item.(map[string]any)
			if _, exists := b["order"]; exists {
				t.Fatal("internal order exposed")
			}
			if b["revision_id"] != contracts.ID("rev") || b["text"] != "中文正文 " {
				t.Fatal("content changed")
			}
		}
		if tc.count == 2 && blocks[0].(map[string]any)["id"] != contracts.ID("one") {
			t.Fatal("frozen order lost")
		}
		if _, err := json.Marshal(out); err != nil {
			t.Fatalf("ids=%v payload=%s err=%v", tc.ids, r.Payload, err)
		}
	}
	r.Payload = []byte(`{"blocks":[]}`)
	if _, err := ProjectReadContent(r, nil); err != ErrReadProjection {
		t.Fatal("invalid payload accepted")
	}
}
