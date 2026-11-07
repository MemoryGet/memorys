package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// OCRParser turns an image or scanned PDF into a bounded text projection. OCR
// locators are explicitly marked as OCR: page numbers are reliable for PDFs,
// while character ranges refer to the generated OCR text rather than bytes in
// the source image.
type OCRParser struct {
	Store     contracts.BlobStore
	Converter OfficeConverter
	MaxBytes  int64
}

func (p OCRParser) Parse(ctx context.Context, req contracts.ParseRequest) (contracts.ParseResult, error) {
	profile, err := hex.DecodeString(string(req.Request.ProfileDigest))
	if ctx == nil || p.Store == nil || p.Converter == nil || req.DraftID == "" || req.Request.RequestID == "" || req.Input.ID == "" || req.Request.Deadline.IsZero() || req.Input.Size <= 0 || !req.Options.EnableOCR || err != nil || len(profile) != sha256.Size || hex.EncodeToString(profile) != string(req.Request.ProfileDigest) {
		return contracts.ParseResult{}, ErrParse
	}
	if req.Format != "png" && req.Format != "jpg" && req.Format != "jpeg" && req.Format != "pdf" {
		return contracts.ParseResult{}, ErrParse
	}
	if _, ok := ctx.Deadline(); !ok {
		return contracts.ParseResult{}, ErrParse
	}
	deadline := req.Request.Deadline
	if !req.Scope.ExpiresAt.IsZero() && req.Scope.ExpiresAt.Before(deadline) {
		deadline = req.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	limit := p.MaxBytes
	if limit <= 0 || limit > 8<<20 {
		limit = 8 << 20
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
	if err != nil || converted.Version == "" || converted.Text == "" || len(converted.Text) > 4<<20 || !utf8.ValidString(converted.Text) || ctx.Err() != nil {
		return contracts.ParseResult{}, ErrParse
	}
	pages := converted.Pages
	if len(pages) == 0 {
		pages = []string{converted.Text}
	}
	if strings.Join(pages, "\n\n") != converted.Text || len(pages) > 200 {
		return contracts.ParseResult{}, ErrParse
	}
	if len(converted.OCRConfidence) != 0 && len(converted.OCRConfidence) != len(pages) {
		return contracts.ParseResult{}, ErrParse
	}
	blocks := make([]contracts.ParsedBlock, 0, len(pages))
	for index, text := range pages {
		if strings.TrimSpace(text) == "" {
			continue
		}
		page := index + 1
		var confidence *float64
		if len(converted.OCRConfidence) > 0 {
			value := converted.OCRConfidence[index]
			if value < 0 || value > 100 {
				return contracts.ParseResult{}, ErrParse
			}
			confidence = &value
		}
		id := sha256.Sum256([]byte(string(req.DraftID) + "\x00" + string(req.Input.SHA256) + "\x00ocr\x00" + req.Format + "\x00" + string(req.Request.ProfileDigest) + "\x00" + converted.Version + "\x00" + text))
		blocks = append(blocks, contracts.ParsedBlock{ID: contracts.ID(hex.EncodeToString(id[:])), Order: len(blocks), Content: contracts.Content{Kind: contracts.TextBlock, Text: text, Source: contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: contracts.Locator{Precision: "ocr", Page: &page, Confidence: confidence, Characters: &contracts.CharacterRange{Start: 0, End: utf8.RuneCountInString(text)}}}}})
	}
	if len(blocks) == 0 {
		return contracts.ParseResult{}, ErrParse
	}
	return contracts.ParseResult{ParserVersion: converted.Version, Blocks: blocks, Diagnostics: []contracts.Diagnostic{{Code: "document_ocr_projection", Severity: "warning", SafeMessage: "OCR text is searchable and page-aware, but character positions refer to the generated OCR projection"}}}, nil
}
