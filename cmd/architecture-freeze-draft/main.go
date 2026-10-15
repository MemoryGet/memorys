// Golden-vector bridge: typed native draft -> standard frozen bytes/hashes.
// Not a submit endpoint; no authorization or authoritative transaction.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func run() error {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return errors.New("invalid frozen fixture")
	}
	if _, err := canonical.JSON(raw); err != nil {
		return errors.New("invalid frozen fixture")
	}
	var input struct {
		Format string          `json:"format"`
		Blocks json.RawMessage `json:"blocks"`
		Blobs  []struct {
			ID        contracts.ID     `json:"id"`
			SHA256    contracts.Digest `json:"sha256"`
			Size      int64            `json:"size"`
			MediaType string           `json:"media_type"`
		} `json:"blobs"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Format != "akos.freeze/1" || input.Blobs == nil {
		return errors.New("invalid frozen fixture")
	}
	wrapped, err := json.Marshal(map[string]any{"request_id": "fixture-request", "invocation_id": "fixture-invocation", "result": map[string]any{"blocks": input.Blocks, "diagnostics": []any{}, "parser_version": "fixture"}})
	if err != nil {
		return err
	}
	parsed, err := modulewire.DecodeParseResult(wrapped, "fixture-request", "fixture-invocation")
	if err != nil {
		return fmt.Errorf("fixture wire mapping: %w", err)
	}
	draft := contracts.Draft{ID: "fixture-draft", DocumentID: "fixture-document", RowVersion: 1, State: "draft"}
	for _, block := range parsed.Blocks {
		draft.Blocks = append(draft.Blocks, contracts.DraftBlock{ID: block.ID, DraftID: draft.ID, RowVersion: 1, Order: block.Order, Content: block.Content})
	}
	for _, blob := range input.Blobs {
		draft.Blobs = append(draft.Blobs, contracts.BlobRef{ID: blob.ID, SHA256: blob.SHA256, Size: blob.Size, MediaType: blob.MediaType})
	}
	frozen, err := document.BuildFrozenContent(draft)
	if err != nil {
		return fmt.Errorf("fixture freeze mapping: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"payload": string(frozen.Payload), "hashes": map[string]any{"format": frozen.HashFormat, "content_hash": frozen.ContentHash, "blocks_hash": frozen.BlocksHash}, "production_acceptance": false})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "native draft freezing rejected:", err)
		os.Exit(1)
	}
}
