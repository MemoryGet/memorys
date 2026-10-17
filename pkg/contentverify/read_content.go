package contentverify

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// TextReadVerifier supports whole nonempty text blocks with coarse revision or
// original anchors, at most 1000 blocks/4MiB. Original text must occur in actual
// independently authorized, hash-verified bytes. Structured content, derived
// sources and precise geometry need other implementations of the standard port.
// No retrieval index is consulted.
type TextReadVerifier struct {
	Reader    contracts.FrozenContentReader
	Originals contracts.BlobStore
	Control   OriginalReadControl
	Projector OriginalTextProjector
}

var _ contracts.ReadContentVerifier = TextReadVerifier{}

func (v TextReadVerifier) VerifyReadContent(ctx context.Context, q contracts.FrozenContentReadRequest, expected contracts.FrozenRevisionFact, record contracts.FrozenContentRecord) error {
	if ctx == nil || ctx.Err() != nil || record.DocumentID != q.DocumentID || record.RevisionID != q.RevisionID || len(record.Payload) == 0 || len(record.Payload) > 4<<20 {
		return ErrContent
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrContent
	}
	// Own bytes and scope before invoking any replaceable dependency.
	payload := append([]byte(nil), record.Payload...)
	q.Scope.KnowledgeBaseIDs = append([]contracts.ID(nil), q.Scope.KnowledgeBaseIDs...)
	h, err := canonical.Frozen(payload)
	if err != nil || h.Format != expected.HashFormat || h.ContentHash != string(expected.ContentHash) || h.BlocksHash != string(expected.BlocksHash) {
		return ErrContent
	}
	var content struct {
		Blocks []struct {
			ID         contracts.ID    `json:"id"`
			Kind       string          `json:"kind"`
			Text       string          `json:"text"`
			Source     json.RawMessage `json:"source_anchor"`
			Structured json.RawMessage `json:"structured_content"`
		} `json:"blocks"`
	}
	if json.Unmarshal(payload, &content) != nil || content.Blocks == nil || len(content.Blocks) > 1000 {
		return ErrContent
	}
	seen := map[contracts.ID]bool{}
	revisions := []contracts.Evidence{}
	originals := map[contracts.ID][]contracts.Evidence{}
	var order []contracts.ID
	// Complete capability preflight before any original bytes leave storage.
	for i, b := range content.Blocks {
		if b.ID == "" || seen[b.ID] {
			return ErrContent
		}
		seen[b.ID] = true
		if b.Kind != "text" || b.Text == "" || len(b.Structured) != 0 {
			return ErrUnsupportedEvidence
		}
		var source struct {
			Kind    string       `json:"kind"`
			BlobID  contracts.ID `json:"blob_id"`
			BlockID contracts.ID `json:"revision_block_id"`
		}
		if json.Unmarshal(b.Source, &source) != nil {
			return ErrContent
		}
		a := contracts.SourceAnchor{Kind: source.Kind, Locator: contracts.Locator{Precision: "coarse"}}
		anchor := map[string]any{"kind": source.Kind, "locator": map[string]any{"precision": "coarse"}}
		switch source.Kind {
		case "revision":
			if source.BlockID != b.ID || source.BlobID != "" {
				return ErrUnsupportedEvidence
			}
			a.RevisionBlockID = b.ID
			anchor["revision_block_id"] = b.ID
		case "original":
			if source.BlobID == "" || source.BlockID != "" {
				return ErrUnsupportedEvidence
			}
			a.BlobID = source.BlobID
			anchor["blob_id"] = source.BlobID
		default:
			return ErrUnsupportedEvidence
		}
		want, _ := json.Marshal(anchor)
		want, _ = canonical.JSON(want)
		actual, err := canonical.JSON(b.Source)
		if err != nil || !bytes.Equal(actual, want) {
			return ErrUnsupportedEvidence
		}
		e := contracts.Evidence{Citation: contracts.Citation{DocumentID: record.DocumentID, RevisionID: record.RevisionID, BlockID: b.ID, Source: a}, SourceExcerpt: b.Text, Rank: i + 1}
		if a.Kind == "revision" {
			revisions = append(revisions, e)
		} else {
			if _, exists := originals[a.BlobID]; !exists {
				order = append(order, a.BlobID)
			}
			originals[a.BlobID] = append(originals[a.BlobID], e)
		}
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, q.Scope.ExpiresAt)
	defer expire()
	if err := VerifyFrozen(ctx, v.Reader, q, expected); err != nil {
		return err
	}
	if len(revisions) > 0 {
		if err := VerifyTextEvidence(ctx, v.Reader, q, expected, revisions); err != nil {
			return err
		}
	}
	for _, blobID := range order {
		if err := VerifyOriginalTextEvidence(ctx, v.Reader, v.Originals, v.Control, v.Projector, q, expected, originals[blobID]); err != nil {
			return err
		}
	}
	return ctx.Err()
}
