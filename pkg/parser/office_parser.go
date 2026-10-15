package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type MarkdownConversion struct{ Text, Version string }
type OfficeConverter interface {
	Convert(context.Context, string, []byte) (MarkdownConversion, error)
}
type OfficeParser struct {
	Store     contracts.BlobStore
	Converter OfficeConverter
	MaxBytes  int64
}

func (p OfficeParser) Parse(ctx context.Context, req contracts.ParseRequest) (contracts.ParseResult, error) {
	profile, err := hex.DecodeString(string(req.Request.ProfileDigest))
	if ctx == nil || p.Store == nil || p.Converter == nil || req.DraftID == "" || req.Request.RequestID == "" || req.Input.ID == "" || req.Request.Deadline.IsZero() || req.Input.Size <= 0 || req.Options.EnableOCR || err != nil || len(profile) != sha256.Size || hex.EncodeToString(profile) != string(req.Request.ProfileDigest) {
		return contracts.ParseResult{}, ErrParse
	}
	if _, ok := ctx.Deadline(); !ok {
		return contracts.ParseResult{}, ErrParse
	}
	if req.Format != "docx" && req.Format != "xlsx" && req.Format != "pptx" && req.Format != "pdf" {
		return contracts.ParseResult{}, ErrParse
	}
	deadline := req.Request.Deadline
	if !req.Scope.ExpiresAt.IsZero() && req.Scope.ExpiresAt.Before(deadline) {
		deadline = req.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if ctx.Err() != nil {
		return contracts.ParseResult{}, ErrParse
	}
	limit := p.MaxBytes
	if limit <= 0 {
		limit = 8 << 20
	}
	if limit > 8<<20 || req.Input.Size > limit {
		return contracts.ParseResult{}, ErrParse
	}
	stream, err := p.Store.Open(ctx, contracts.BlobReadRequest{Request: req.Request, Scope: req.Scope, Blob: req.Input})
	if err != nil {
		return contracts.ParseResult{}, ErrParse
	}
	defer stream.Close()
	raw, err := io.ReadAll(io.LimitReader(stream, limit+1))
	digest := sha256.Sum256(raw)
	if err != nil || int64(len(raw)) != req.Input.Size || hex.EncodeToString(digest[:]) != string(req.Input.SHA256) || ctx.Err() != nil {
		return contracts.ParseResult{}, ErrParse
	}
	converted, err := p.Converter.Convert(ctx, req.Format, raw)
	if err != nil || converted.Text == "" || len(converted.Text) > 4<<20 || !utf8.ValidString(converted.Text) || converted.Version == "" || ctx.Err() != nil {
		return contracts.ParseResult{}, ErrParse
	}
	id := sha256.Sum256([]byte(string(req.DraftID) + "\x00" + string(req.Input.SHA256) + "\x00" + req.Format + "\x00" + string(req.Request.ProfileDigest) + "\x00" + converted.Version))
	return contracts.ParseResult{ParserVersion: converted.Version, Blocks: []contracts.ParsedBlock{{ID: contracts.ID(hex.EncodeToString(id[:])), Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: converted.Text, Source: contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: contracts.Locator{Precision: "coarse"}}}}}, Diagnostics: []contracts.Diagnostic{{Code: "document_projection_coarse", Severity: "warning", SafeMessage: "Converted text is a candidate projection; exact pages, cells and structured tables are not available"}}}, nil
}
