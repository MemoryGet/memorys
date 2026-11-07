package document

import (
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestCompareDraftsReportsBlockChanges(t *testing.T) {
	before := editableDraft()
	after := editableDraft()
	after.ID = "draft-new"
	after.RowVersion = 3
	after.Blocks[0].DraftID = after.ID
	after.Blocks[0].Content.Text = "已修改"
	after.Blocks = append(after.Blocks, contracts.DraftBlock{ID: "added", DraftID: after.ID, RowVersion: 1, Order: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: "新增", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "added", Locator: contracts.Locator{Precision: "coarse"}}}})
	diff, err := CompareDrafts(before, after)
	if err != nil || len(diff.Changes) != 2 || diff.Changes[0].Kind != "changed" || diff.Changes[1].Kind != "added" {
		t.Fatalf("diff: %+v, %v", diff, err)
	}
}

func TestCompareDraftsRejectsDifferentDocuments(t *testing.T) {
	before, after := editableDraft(), editableDraft()
	after.DocumentID = "other"
	if _, err := CompareDrafts(before, after); err == nil {
		t.Fatal("different document compared")
	}
}
