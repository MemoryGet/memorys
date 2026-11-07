package indexsnapshot

import (
	"encoding/json"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"math"
	"slices"
	"unicode/utf8"
)

// Copy makes an independent structural copy with the original JSON size and
// normalization rules. It does not validate a snapshot or grant authority.
func Copy(input contracts.IndexSnapshot) (contracts.IndexSnapshot, error) {
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 4<<20 {
		return contracts.IndexSnapshot{}, ErrSnapshot
	}
	return copyEncoded(input, raw)
}
func copyEncoded(input contracts.IndexSnapshot, raw []byte) (contracts.IndexSnapshot, error) {
	if owned, ok := copySnapshotFields(input); ok {
		return owned, nil
	}
	var owned contracts.IndexSnapshot
	if json.Unmarshal(raw, &owned) != nil {
		return owned, ErrSnapshot
	}
	return owned, nil
}
func validText(values ...string) bool {
	for _, s := range values {
		if !utf8.ValidString(s) {
			return false
		}
	}
	return true
}
func copyPointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func copyLocator(l contracts.Locator) (contracts.Locator, bool) {
	if !validText(l.Precision, l.Worksheet, l.CellRange) {
		return l, false
	}
	if l.Box != nil {
		for _, v := range []float64{l.Box.Left, l.Box.Top, l.Box.Right, l.Box.Bottom} {
			if v == 0 && math.Signbit(v) {
				return l, false
			}
		}
	}
	l.Page = copyPointer(l.Page)
	l.Slide = copyPointer(l.Slide)
	l.Characters = copyPointer(l.Characters)
	l.Box = copyPointer(l.Box)
	l.ParagraphPath = slices.Clone(l.ParagraphPath)
	return l, true
}
func copyAnchor(a contracts.SourceAnchor) (contracts.SourceAnchor, bool) {
	if !validText(a.Kind, string(a.BlobID), string(a.RevisionBlockID)) {
		return a, false
	}
	var ok bool
	a.Locator, ok = copyLocator(a.Locator)
	if !ok {
		return a, false
	}
	a.DerivedFrom = slices.Clone(a.DerivedFrom)
	for i := range a.DerivedFrom {
		if !validText(string(a.DerivedFrom[i].BlobID)) {
			return a, false
		}
		a.DerivedFrom[i].Locator, ok = copyLocator(a.DerivedFrom[i].Locator)
		if !ok {
			return a, false
		}
	}
	return a, true
}
func copySnapshotFields(s contracts.IndexSnapshot) (contracts.IndexSnapshot, bool) {
	if !validText(s.Format, string(s.SpaceID), string(s.KnowledgeBaseID), string(s.Checksum)) {
		return s, false
	}
	s.Generations = slices.Clone(s.Generations)
	s.Records = slices.Clone(s.Records)
	s.ChunkFences = slices.Clone(s.ChunkFences)
	for _, g := range s.Generations {
		if !validText(string(g.SpaceID), string(g.KnowledgeBaseID), string(g.DocumentID), string(g.RevisionID), string(g.GenerationID), string(g.BackendEpochID)) {
			return s, false
		}
	}
	for _, f := range s.ChunkFences {
		if !validText(string(f.GenerationID), string(f.ChunkID)) {
			return s, false
		}
	}
	for i := range s.Records {
		r := &s.Records[i]
		if !validText(string(r.SpaceID), string(r.KnowledgeBaseID), string(r.DocumentID), string(r.GenerationID), string(r.BackendEpochID), string(r.Chunk.ID), string(r.Chunk.RevisionID), r.Chunk.Text, string(r.Chunk.ProfileDigest), string(r.Chunk.ContentHash), string(r.Citation.DocumentID), string(r.Citation.RevisionID), string(r.Citation.BlockID)) {
			return s, false
		}
		r.Chunk.Spans = slices.Clone(r.Chunk.Spans)
		for j := range r.Chunk.Spans {
			if !validText(string(r.Chunk.Spans[j].BlockID)) {
				return s, false
			}
			r.Chunk.Spans[j].Characters = copyPointer(r.Chunk.Spans[j].Characters)
		}
		var ok bool
		r.Citation.Source, ok = copyAnchor(r.Citation.Source)
		if !ok {
			return s, false
		}
	}
	return s, true
}
