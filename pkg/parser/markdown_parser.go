package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"io"
	"unicode/utf8"
)

var ErrParse = errors.New("markdown parsing rejected")

type MarkdownParser struct {
	Store    contracts.BlobStore
	MaxBytes int64
}

func (p MarkdownParser) Parse(ctx context.Context, req contracts.ParseRequest) (contracts.ParseResult, error) {
	if ctx == nil || p.Store == nil || req.Format != "markdown" || req.DraftID == "" || req.Input.Size < 0 {
		return contracts.ParseResult{}, ErrParse
	}
	if _, ok := ctx.Deadline(); !ok {
		return contracts.ParseResult{}, ErrParse
	}
	ctx, cancel := context.WithDeadline(ctx, req.Request.Deadline)
	defer cancel()
	limit := p.MaxBytes
	if limit <= 0 {
		limit = 8 << 20
	}
	if req.Input.Size > limit {
		return contracts.ParseResult{}, ErrParse
	}
	stream, err := p.Store.Open(ctx, contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: req.Input})
	if err != nil {
		return contracts.ParseResult{}, ErrParse
	}
	defer stream.Close()
	raw, err := io.ReadAll(io.LimitReader(stream, limit+1))
	hash := sha256.Sum256(raw)
	if err != nil || int64(len(raw)) != req.Input.Size || int64(len(raw)) > limit || hex.EncodeToString(hash[:]) != string(req.Input.SHA256) || !utf8.Valid(raw) || len(raw) == 0 {
		return contracts.ParseResult{}, ErrParse
	}
	if ctx.Err() != nil {
		return contracts.ParseResult{}, ctx.Err()
	}
	id := sha256.Sum256([]byte(string(req.DraftID) + "\x00" + string(req.Input.SHA256)))
	// Preserve exact text, whitespace and line endings for stable source offsets.
	return contracts.ParseResult{ParserVersion: "markdown-v1", Blocks: []contracts.ParsedBlock{{ID: contracts.ID(hex.EncodeToString(id[:])), Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: string(raw), Source: contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: contracts.Locator{Precision: "exact", Characters: &contracts.CharacterRange{Start: 0, End: utf8.RuneCount(raw)}}}}}}}, nil
}
