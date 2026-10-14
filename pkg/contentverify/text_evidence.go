package contentverify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrUnsupportedEvidence = errors.New("evidence locator verification unsupported")

// VerifyTextEvidence verifies revision-text citations against freshly read,
// hash-verified frozen content. It supports exact Unicode code point ranges or
// coarse substring citations, never original-file geometry or structured media.
// Unsupported evidence is an error, not permission to omit its verification.
func VerifyTextEvidence(ctx context.Context, reader contracts.FrozenContentReader, request contracts.FrozenContentReadRequest, expected contracts.FrozenRevisionFact, evidence []contracts.Evidence) error {
	if ctx == nil || len(evidence) == 0 || len(evidence) > 1000 {
		return ErrContent
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrContent
	}
	fixed := make([]contracts.Evidence, len(evidence))
	for i, e := range evidence {
		if e.Citation.DocumentID != expected.DocumentID || e.Citation.RevisionID != expected.ID || e.Citation.BlockID == "" || e.SourceExcerpt == "" || !utf8.ValidString(e.SourceExcerpt) || e.Rank < 1 {
			return ErrContent
		}
		a, l := e.Citation.Source, e.Citation.Source.Locator
		if a.Kind != "revision" || a.BlobID != "" || a.RevisionBlockID != e.Citation.BlockID || len(a.DerivedFrom) != 0 || l.Page != nil || l.Slide != nil || l.Worksheet != "" || l.CellRange != "" || l.ParagraphPath != nil || l.Box != nil {
			return ErrUnsupportedEvidence
		}
		fixed[i] = e
		if l.Characters != nil {
			r := *l.Characters
			fixed[i].Citation.Source.Locator.Characters = &r
		}
	}
	ctx, cancel := context.WithDeadline(ctx, request.Request.Deadline)
	defer cancel()
	payload, err := readVerifiedFrozen(ctx, reader, request, expected)
	if err != nil {
		return err
	}
	var content struct {
		Blocks []struct {
			ID   contracts.ID `json:"id"`
			Kind string       `json:"kind"`
			Text string       `json:"text"`
		} `json:"blocks"`
	}
	if json.Unmarshal(payload, &content) != nil {
		return ErrContent
	}
	byID := make(map[contracts.ID]int, len(content.Blocks))
	for i, b := range content.Blocks {
		if b.ID == "" || !utf8.ValidString(string(b.ID)) {
			return ErrContent
		}
		if _, exists := byID[b.ID]; exists {
			return ErrContent
		}
		byID[b.ID] = i
	}
	for _, e := range fixed {
		if err := ctx.Err(); err != nil {
			return err
		}
		i, exists := byID[e.Citation.BlockID]
		if !exists {
			return ErrContent
		}
		b := content.Blocks[i]
		a := e.Citation.Source
		l := a.Locator
		if b.Kind != "text" || a.Kind != "revision" || a.BlobID != "" || a.RevisionBlockID != b.ID || len(a.DerivedFrom) != 0 || l.Page != nil || l.Slide != nil || l.Worksheet != "" || l.CellRange != "" || l.ParagraphPath != nil || l.Box != nil {
			return ErrUnsupportedEvidence
		}
		switch l.Precision {
		case "exact":
			if l.Characters == nil {
				return ErrUnsupportedEvidence
			}
			r := l.Characters
			text := []rune(b.Text)
			if r.Start < 0 || r.End <= r.Start || r.End > len(text) || string(text[r.Start:r.End]) != e.SourceExcerpt {
				return ErrContent
			}
		case "coarse":
			if l.Characters != nil {
				return ErrUnsupportedEvidence
			}
			if !strings.Contains(b.Text, e.SourceExcerpt) {
				return ErrContent
			}
		default:
			return ErrUnsupportedEvidence
		}
	}
	if !request.Scope.ExpiresAt.After(time.Now()) {
		return ErrContent
	}
	return ctx.Err()
}
