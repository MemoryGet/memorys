package document

import (
	"encoding/json"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// FromParsedBlocks produces draft edits, not an approved immutable Revision.
// Metadata transactions assign row versions and freeze content on submission.
func FromParsedBlocks(draftID contracts.ID, parsed []contracts.ParsedBlock) ([]contracts.DraftBlock, error) {
	if draftID == "" {
		return nil, errors.New("draft required")
	}
	raw, err := json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	var owned []contracts.ParsedBlock
	if json.Unmarshal(raw, &owned) != nil {
		return nil, errors.New("invalid parsed content")
	}
	blocks := make([]contracts.DraftBlock, 0, len(owned))
	seen := map[contracts.ID]bool{}
	for _, p := range owned {
		if p.ID == "" || seen[p.ID] {
			return nil, errors.New("invalid parsed blocks")
		}
		seen[p.ID] = true
		blocks = append(blocks, contracts.DraftBlock{ID: p.ID, DraftID: draftID, Order: p.Order, Content: p.Content})
	}
	return blocks, nil
}
