package document

import (
	"errors"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func editableDraft() contracts.Draft {
	return contracts.Draft{ID: "draft", DocumentID: "doc", RowVersion: 2, State: "draft", Blocks: []contracts.DraftBlock{{ID: "block", DraftID: "draft", RowVersion: 3, Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: "原文", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "coarse"}}}}}}
}

func TestApplyDraftEditIncrementsChangedBlockOnly(t *testing.T) {
	draft := editableDraft()
	blocks := append([]contracts.DraftBlock(nil), draft.Blocks...)
	blocks[0].Content.Text = "新文"
	updated, err := ApplyDraftEdit(draft, blocks)
	if err != nil || updated.RowVersion != 3 || updated.Blocks[0].RowVersion != 4 || updated.Blocks[0].Order != 0 {
		t.Fatalf("updated draft: %+v, %v", updated, err)
	}
}

func TestApplyDraftEditRejectsStaleBlockAndForeignID(t *testing.T) {
	draft := editableDraft()
	stale := append([]contracts.DraftBlock(nil), draft.Blocks...)
	stale[0].RowVersion = 2
	if _, err := ApplyDraftEdit(draft, stale); !errors.Is(err, ErrDraftEditConflict) {
		t.Fatalf("stale block accepted: %v", err)
	}
	foreign := append([]contracts.DraftBlock(nil), draft.Blocks...)
	foreign[0].DraftID = "other"
	if _, err := ApplyDraftEdit(draft, foreign); err == nil {
		t.Fatal("foreign block accepted")
	}
}
