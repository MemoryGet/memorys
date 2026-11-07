package document

import (
	"errors"
	"math"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrDraftEdit = errors.New("draft edit rejected")
var ErrDraftEditConflict = errors.New("draft edit conflicts with current version")

// ApplyDraftEdit applies a complete ordered block replacement to a mutable
// draft. The caller still has to persist the returned value with a database
// CAS and an idempotent audit/outbox receipt.
func ApplyDraftEdit(current contracts.Draft, replacement []contracts.DraftBlock) (contracts.Draft, error) {
	if current.ID == "" || current.DocumentID == "" || current.RowVersion == 0 || current.RowVersion == math.MaxUint64 || current.State != "draft" || len(replacement) > 100000 {
		return contracts.Draft{}, ErrDraftEdit
	}
	byID := make(map[contracts.ID]contracts.DraftBlock, len(current.Blocks))
	for _, block := range current.Blocks {
		if block.ID == "" || block.DraftID != current.ID || block.RowVersion == 0 || block.RowVersion == math.MaxUint64 || byID[block.ID].ID != "" {
			return contracts.Draft{}, ErrDraftEdit
		}
		byID[block.ID] = block
	}
	seen := make(map[contracts.ID]bool, len(replacement))
	blocks := make([]contracts.DraftBlock, 0, len(replacement))
	for index, block := range replacement {
		if block.ID == "" || block.DraftID != current.ID || block.RowVersion == 0 || seen[block.ID] {
			return contracts.Draft{}, ErrDraftEdit
		}
		seen[block.ID] = true
		block.Order = index
		if old, exists := byID[block.ID]; exists {
			if block.RowVersion != old.RowVersion {
				return contracts.Draft{}, ErrDraftEditConflict
			}
			if reflect.DeepEqual(old.Content, block.Content) {
				block.Content = old.Content
			} else {
				block.RowVersion++
			}
		} else if block.RowVersion != 1 {
			return contracts.Draft{}, ErrDraftEditConflict
		}
		blocks = append(blocks, block)
	}
	updated := current
	updated.RowVersion++
	updated.Blocks = blocks
	if _, err := BuildFrozenContent(updated); err != nil {
		return contracts.Draft{}, ErrDraftEdit
	}
	return updated, nil
}
