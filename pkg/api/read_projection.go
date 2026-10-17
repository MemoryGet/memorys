package api

import (
	"encoding/json"
	"errors"
	"math/big"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrReadProjection = errors.New("read content projection unavailable")

// ProjectReadContent maps already authorized standard frozen content to partner
// blocks/citations. It performs no authentication, source verification or send.
// Nil IDs select all; an explicit empty list selects none. Order remains the
// frozen order; missing or repeated selections are rejected rather than skipped.
func ProjectReadContent(record contracts.FrozenContentRecord, ids []contracts.ID) (map[string]any, error) {
	if !partnerID(record.DocumentID) || !partnerID(record.RevisionID) || len(record.Payload) > 4<<20 || len(ids) > 1000 {
		return nil, ErrReadProjection
	}
	if _, err := canonical.Frozen(record.Payload); err != nil {
		return nil, ErrReadProjection
	}
	var envelope struct {
		Blocks []map[string]json.RawMessage `json:"blocks"`
	}
	if json.Unmarshal(record.Payload, &envelope) != nil || len(envelope.Blocks) == 0 || len(envelope.Blocks) > 10000 {
		return nil, ErrReadProjection
	}
	selected := map[contracts.ID]bool{}
	for _, id := range ids {
		if !partnerID(id) || selected[id] {
			return nil, ErrReadProjection
		}
		selected[id] = true
	}
	blocks, citations := []any{}, []any{}
	seen := map[contracts.ID]bool{}
	for index, raw := range envelope.Blocks {
		for key := range raw {
			if key != "id" && key != "order" && key != "kind" && key != "text" && key != "source_anchor" && key != "structured_content" {
				return nil, ErrReadProjection
			}
		}
		var id contracts.ID
		order, orderOK := new(big.Rat).SetString(string(raw["order"]))
		var kind, text string
		if json.Unmarshal(raw["id"], &id) != nil || !partnerID(id) || seen[id] || !orderOK || !order.IsInt() || !order.Num().IsInt64() || order.Num().Int64() != int64(index) || json.Unmarshal(raw["kind"], &kind) != nil || json.Unmarshal(raw["text"], &text) != nil {
			return nil, ErrReadProjection
		}
		seen[id] = true
		var source map[string]json.RawMessage
		if json.Unmarshal(raw["source_anchor"], &source) != nil || source == nil {
			return nil, ErrReadProjection
		}
		_, structured := raw["structured_content"]
		if kind == "text" && structured || kind != "text" && kind != "table" && kind != "image" && kind != "audio" || kind != "text" && !structured {
			return nil, ErrReadProjection
		}
		if ids != nil && !selected[id] {
			continue
		}
		block := map[string]any{"id": id, "revision_id": record.RevisionID, "kind": kind, "text": text, "source_anchor": raw["source_anchor"]}
		if structured {
			block["structured_content"] = raw["structured_content"]
		}
		blocks = append(blocks, block)
		citations = append(citations, map[string]any{"document_id": record.DocumentID, "revision_id": record.RevisionID, "block_id": id, "source_anchor": raw["source_anchor"]})
		delete(selected, id)
	}
	if len(selected) != 0 {
		return nil, ErrReadProjection
	}
	return map[string]any{"document_id": record.DocumentID, "revision_id": record.RevisionID, "blocks": blocks, "citations": citations}, nil
}
