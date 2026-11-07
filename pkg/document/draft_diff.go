package document

import (
	"errors"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrDraftDiff = errors.New("draft comparison rejected")

type DraftBlockChange struct {
	BlockID contracts.ID
	Kind    string // added, removed, changed, unchanged
	Before  *contracts.DraftBlock
	After   *contracts.DraftBlock
}

type DraftDiff struct {
	FromID, ToID           contracts.ID
	FromVersion, ToVersion uint64
	Changes                []DraftBlockChange
}

// CompareDrafts compares two validated mutable snapshots without merging or
// applying either one. Block IDs are the stable identity; order changes are
// reported as changed so callers can render a meaningful conflict preview.
func CompareDrafts(before, after contracts.Draft) (DraftDiff, error) {
	if before.ID == "" || after.ID == "" || before.DocumentID == "" || before.DocumentID != after.DocumentID || before.RowVersion == 0 || after.RowVersion == 0 || before.State == "submitted" || after.State == "submitted" {
		return DraftDiff{}, ErrDraftDiff
	}
	left := make(map[contracts.ID]contracts.DraftBlock, len(before.Blocks))
	right := make(map[contracts.ID]contracts.DraftBlock, len(after.Blocks))
	for _, block := range before.Blocks {
		if block.ID == "" || block.DraftID != before.ID || block.RowVersion == 0 || left[block.ID].ID != "" {
			return DraftDiff{}, ErrDraftDiff
		}
		left[block.ID] = block
	}
	for _, block := range after.Blocks {
		if block.ID == "" || block.DraftID != after.ID || block.RowVersion == 0 || right[block.ID].ID != "" {
			return DraftDiff{}, ErrDraftDiff
		}
		right[block.ID] = block
	}
	changes := make([]DraftBlockChange, 0, len(left)+len(right))
	for _, block := range before.Blocks {
		current, ok := right[block.ID]
		if !ok {
			copyBefore := block
			changes = append(changes, DraftBlockChange{BlockID: block.ID, Kind: "removed", Before: &copyBefore})
			continue
		}
		copyBefore, copyAfter := block, current
		kind := "unchanged"
		if block.Order != current.Order || !reflect.DeepEqual(block.Content, current.Content) || block.RowVersion != current.RowVersion {
			kind = "changed"
		}
		changes = append(changes, DraftBlockChange{BlockID: block.ID, Kind: kind, Before: &copyBefore, After: &copyAfter})
	}
	for _, block := range after.Blocks {
		if _, ok := left[block.ID]; !ok {
			copyAfter := block
			changes = append(changes, DraftBlockChange{BlockID: block.ID, Kind: "added", After: &copyAfter})
		}
	}
	return DraftDiff{FromID: before.ID, ToID: after.ID, FromVersion: before.RowVersion, ToVersion: after.RowVersion, Changes: changes}, nil
}
