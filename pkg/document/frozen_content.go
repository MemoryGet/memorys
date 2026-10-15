package document

import (
	"encoding/json"
	"errors"
	"math"
	"mime"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrFrozenContent = errors.New("draft content cannot be frozen")

type FrozenContent struct {
	Payload                 []byte
	HashFormat              string
	ContentHash, BlocksHash contracts.Digest
}

type frozenBuilder struct {
	blobs  map[contracts.ID]contracts.BlobRef
	blocks map[contracts.ID]bool
}

// BuildFrozenContent prepares immutable bytes/identities, not a committed
// Revision. Caller must still verify current ownership, original/media bytes,
// source accuracy and perform the submission transaction with CAS/audit/outbox.
func BuildFrozenContent(input contracts.Draft) (FrozenContent, error) {
	if input.ID == "" || input.DocumentID == "" || input.RowVersion == 0 || input.State != "draft" || len(input.Blocks) == 0 || len(input.Blocks) > 10000 || len(input.Blobs) > 10000 || !frozenUTF8(reflect.ValueOf(input)) {
		return FrozenContent{}, ErrFrozenContent
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 8<<20 {
		return FrozenContent{}, ErrFrozenContent
	}
	var draft contracts.Draft
	if json.Unmarshal(raw, &draft) != nil {
		return FrozenContent{}, ErrFrozenContent
	}
	b := frozenBuilder{blobs: map[contracts.ID]contracts.BlobRef{}, blocks: map[contracts.ID]bool{}}
	sort.Slice(draft.Blobs, func(i, j int) bool { return draft.Blobs[i].ID < draft.Blobs[j].ID })
	blobs := make([]any, 0, len(draft.Blobs))
	for _, blob := range draft.Blobs {
		if !frozenID(blob.ID) || b.blobs[blob.ID].ID != "" || !candidateDigest(blob.SHA256) || blob.Size < 0 || blob.MediaType == "" {
			return FrozenContent{}, ErrFrozenContent
		}
		if _, _, err := mime.ParseMediaType(blob.MediaType); err != nil {
			return FrozenContent{}, ErrFrozenContent
		}
		b.blobs[blob.ID] = blob
		blobs = append(blobs, frozenBlob(blob))
	}
	for index, block := range draft.Blocks {
		if !frozenID(block.ID) || block.DraftID != draft.ID || block.RowVersion == 0 || block.Order != index || b.blocks[block.ID] {
			return FrozenContent{}, ErrFrozenContent
		}
		b.blocks[block.ID] = true
	}
	blocks := make([]any, 0, len(draft.Blocks))
	useful := false
	for _, block := range draft.Blocks {
		mapped, valid, err := b.content(block.Content)
		if err != nil {
			return FrozenContent{}, err
		}
		mapped["id"] = block.ID
		mapped["order"] = block.Order
		blocks = append(blocks, mapped)
		useful = useful || valid
	}
	if !useful {
		return FrozenContent{}, ErrFrozenContent
	}
	raw, err = json.Marshal(map[string]any{"format": canonical.FrozenFormat, "blocks": blocks, "blobs": blobs})
	if err != nil {
		return FrozenContent{}, ErrFrozenContent
	}
	payload, err := canonical.JSON(raw)
	if err != nil {
		return FrozenContent{}, ErrFrozenContent
	}
	hashes, err := canonical.Frozen(payload)
	if err != nil {
		return FrozenContent{}, ErrFrozenContent
	}
	return FrozenContent{Payload: payload, HashFormat: hashes.Format, ContentHash: contracts.Digest(hashes.ContentHash), BlocksHash: contracts.Digest(hashes.BlocksHash)}, nil
}

func frozenID(id contracts.ID) bool {
	return id != "" && utf8.ValidString(string(id)) && utf8.RuneCountInString(string(id)) <= 200
}
func frozenBlob(b contracts.BlobRef) map[string]any {
	return map[string]any{"id": b.ID, "sha256": b.SHA256, "size": b.Size, "media_type": b.MediaType}
}
func frozenUTF8(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Pointer:
		if !v.IsNil() {
			return frozenUTF8(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !frozenUTF8(v.Field(i)) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !frozenUTF8(v.Index(i)) {
				return false
			}
		}
	}
	return true
}

func (b frozenBuilder) locator(l contracts.Locator) (map[string]any, error) {
	if l.Precision != "exact" && l.Precision != "coarse" {
		return nil, ErrFrozenContent
	}
	r := map[string]any{"precision": l.Precision}
	for name, value := range map[string]*int{"page": l.Page, "slide": l.Slide} {
		if value != nil {
			if *value < 1 {
				return nil, ErrFrozenContent
			}
			r[name] = *value
		}
	}
	if l.Worksheet != "" {
		r["worksheet"] = l.Worksheet
	}
	if l.CellRange != "" {
		if l.Worksheet == "" {
			return nil, ErrFrozenContent
		}
		r["cell_range"] = l.CellRange
	}
	if l.ParagraphPath != nil {
		if len(l.ParagraphPath) == 0 {
			return nil, ErrFrozenContent
		}
		for _, v := range l.ParagraphPath {
			if v < 0 {
				return nil, ErrFrozenContent
			}
		}
		r["paragraph_path"] = l.ParagraphPath
	}
	if l.Characters != nil {
		if l.Characters.Start < 0 || l.Characters.End < l.Characters.Start {
			return nil, ErrFrozenContent
		}
		r["char_start"] = l.Characters.Start
		r["char_end"] = l.Characters.End
	}
	if l.Box != nil {
		box := l.Box
		if l.Page == nil || box.Left < 0 || box.Top < 0 || box.Right > 1 || box.Bottom > 1 || box.Left >= box.Right || box.Top >= box.Bottom {
			return nil, ErrFrozenContent
		}
		for _, v := range []float64{box.Left, box.Top, box.Right, box.Bottom} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, ErrFrozenContent
			}
		}
		r["bbox"] = []float64{box.Left, box.Top, box.Right, box.Bottom}
	}
	return r, nil
}

func (b frozenBuilder) source(s contracts.SourceAnchor) (map[string]any, error) {
	locator, err := b.locator(s.Locator)
	if err != nil {
		return nil, err
	}
	r := map[string]any{"kind": s.Kind, "locator": locator}
	switch s.Kind {
	case "original":
		if b.blobs[s.BlobID].ID == "" || s.RevisionBlockID != "" {
			return nil, ErrFrozenContent
		}
		r["blob_id"] = s.BlobID
	case "revision":
		if !b.blocks[s.RevisionBlockID] || s.BlobID != "" {
			return nil, ErrFrozenContent
		}
		r["revision_block_id"] = s.RevisionBlockID
	default:
		return nil, ErrFrozenContent
	}
	if s.DerivedFrom != nil {
		derived := make([]any, 0, len(s.DerivedFrom))
		for _, original := range s.DerivedFrom {
			if b.blobs[original.BlobID].ID == "" {
				return nil, ErrFrozenContent
			}
			l, err := b.locator(original.Locator)
			if err != nil {
				return nil, err
			}
			derived = append(derived, map[string]any{"blob_id": original.BlobID, "locator": l})
		}
		r["derived_from"] = derived
	}
	return r, nil
}

func (b frozenBuilder) content(c contracts.Content) (map[string]any, bool, error) {
	source, err := b.source(c.Source)
	if err != nil {
		return nil, false, err
	}
	r := map[string]any{"kind": c.Kind, "text": c.Text, "source_anchor": source}
	count := 0
	for _, present := range []bool{c.Table != nil, c.Image != nil, c.Audio != nil} {
		if present {
			count++
		}
	}
	useful := strings.TrimSpace(c.Text) != ""
	if c.Kind == contracts.TextBlock {
		if count != 0 {
			return nil, false, ErrFrozenContent
		}
		return r, useful, nil
	}
	if count != 1 {
		return nil, false, ErrFrozenContent
	}
	var payload map[string]any
	switch c.Kind {
	case contracts.TableBlock:
		if c.Table == nil {
			return nil, false, ErrFrozenContent
		}
		payload, err = b.table(*c.Table)
		// Sparse/explicitly empty cells are valid table content; preserve the
		// known structure instead of guessing that an empty string is missing.
		useful = true
	case contracts.ImageBlock:
		if c.Image == nil {
			return nil, false, ErrFrozenContent
		}
		image := c.Image
		if b.blobs[image.Blob.ID] != image.Blob || image.Blob.Size <= 0 || !frozenMediaKind(image.Blob, "image/") || (image.Width == nil) != (image.Height == nil) {
			return nil, false, ErrFrozenContent
		}
		payload = map[string]any{"type": "image", "blob": frozenBlob(image.Blob)}
		if image.Width != nil {
			if *image.Width < 1 || *image.Height < 1 {
				return nil, false, ErrFrozenContent
			}
			payload["width"] = *image.Width
			payload["height"] = *image.Height
		}
		if image.Caption != nil {
			payload["caption"] = *image.Caption
		}
		if image.OCRText != nil {
			payload["ocr_text"] = *image.OCRText
		}
		useful = true
	case contracts.AudioBlock:
		if c.Audio == nil {
			return nil, false, ErrFrozenContent
		}
		audio := c.Audio
		if b.blobs[audio.Blob.ID] != audio.Blob || audio.Blob.Size <= 0 || !frozenMediaKind(audio.Blob, "audio/") || audio.DurationMS < 0 {
			return nil, false, ErrFrozenContent
		}
		segments := append([]contracts.AudioSegment(nil), audio.Segments...)
		sort.SliceStable(segments, func(i, j int) bool {
			if segments[i].StartMS == segments[j].StartMS {
				return segments[i].EndMS < segments[j].EndMS
			}
			return segments[i].StartMS < segments[j].StartMS
		})
		mapped := make([]any, 0, len(segments))
		for _, s := range segments {
			if s.StartMS < 0 || s.EndMS < s.StartMS || s.EndMS > audio.DurationMS {
				return nil, false, ErrFrozenContent
			}
			v := map[string]any{"start_ms": s.StartMS, "end_ms": s.EndMS, "text": s.Text}
			if s.SpeakerLabel != nil {
				v["speaker_label"] = *s.SpeakerLabel
			}
			mapped = append(mapped, v)
		}
		payload = map[string]any{"type": "audio", "blob": frozenBlob(audio.Blob), "duration_ms": audio.DurationMS, "segments": mapped}
		useful = true
	default:
		return nil, false, ErrFrozenContent
	}
	if err != nil {
		return nil, false, err
	}
	r["structured_content"] = payload
	return r, useful, nil
}

func frozenMediaKind(blob contracts.BlobRef, prefix string) bool {
	kind, _, err := mime.ParseMediaType(blob.MediaType)
	return err == nil && strings.HasPrefix(kind, prefix)
}

func (b frozenBuilder) table(t contracts.TableContent) (map[string]any, error) {
	if t.RowCount < 1 || t.ColumnCount < 1 || len(t.Cells) > 100000 {
		return nil, ErrFrozenContent
	}
	cells := append([]contracts.TableCell(nil), t.Cells...)
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].Row == cells[j].Row {
			return cells[i].Column < cells[j].Column
		}
		return cells[i].Row < cells[j].Row
	})
	mapped := make([]any, 0, len(cells))
	for i, c := range cells {
		if c.Row < 0 || c.Column < 0 || c.Row >= t.RowCount || c.Column >= t.ColumnCount || c.RowSpan < 1 || c.ColumnSpan < 1 || c.RowSpan > t.RowCount-c.Row || c.ColumnSpan > t.ColumnCount-c.Column || i > 0 && c.Row == cells[i-1].Row && c.Column == cells[i-1].Column {
			return nil, ErrFrozenContent
		}
		switch c.Role {
		case "data", "column_header", "row_header", "corner_header":
		default:
			return nil, ErrFrozenContent
		}
		r := map[string]any{"row": c.Row, "column": c.Column, "row_span": c.RowSpan, "column_span": c.ColumnSpan, "text": c.Text, "role": c.Role}
		if c.Formula != nil {
			r["formula"] = *c.Formula
		}
		if c.NumberText != nil {
			r["number_text"] = *c.NumberText
		}
		if c.Source != nil {
			source, err := b.source(*c.Source)
			if err != nil {
				return nil, err
			}
			r["source_anchor"] = source
		}
		mapped = append(mapped, r)
	}
	if overlappingCells(cells) {
		return nil, ErrFrozenContent
	}
	r := map[string]any{"type": "table", "row_count": t.RowCount, "column_count": t.ColumnCount, "cells": mapped}
	if t.Caption != nil {
		r["caption"] = *t.Caption
	}
	return r, nil
}
