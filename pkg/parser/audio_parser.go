package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// AudioParser binds a transcript to its source audio blob. The locator is
// marked transcript because character ranges refer to generated text.
type AudioParser struct {
	Store     contracts.BlobStore
	Converter OfficeConverter
	MaxBytes  int64
}

func (p AudioParser) Parse(ctx context.Context, req contracts.ParseRequest) (contracts.ParseResult, error) {
	profile, err := hex.DecodeString(string(req.Request.ProfileDigest))
	if ctx == nil || p.Store == nil || p.Converter == nil || req.DraftID == "" || req.Request.RequestID == "" || req.Input.ID == "" || req.Request.Deadline.IsZero() || req.Input.Size <= 0 || !req.Options.EnableOCR || err != nil || len(profile) != sha256.Size || hex.EncodeToString(profile) != string(req.Request.ProfileDigest) {
		return contracts.ParseResult{}, ErrParse
	}
	if req.Format != "wav" && req.Format != "mp3" && req.Format != "m4a" {
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
	if err != nil || converted.Version == "" || converted.Text == "" || len(converted.Text) > 4<<20 || !utf8.ValidString(converted.Text) || ctx.Err() != nil || strings.TrimSpace(converted.Text) == "" {
		return contracts.ParseResult{}, ErrParse
	}
	if len(converted.AudioSegments) > 0 {
		joined := make([]string, 0, len(converted.AudioSegments))
		blocks := make([]contracts.ParsedBlock, 0, len(converted.AudioSegments))
		previousEnd := int64(0)
		characterOffset := 0
		for index, segment := range converted.AudioSegments {
			text := strings.TrimSpace(segment.Text)
			if text == "" || segment.StartMS < 0 || segment.EndMS < segment.StartMS || index > 0 && segment.StartMS < previousEnd {
				return contracts.ParseResult{}, ErrParse
			}
			joined = append(joined, text)
			startMS, endMS := segment.StartMS, segment.EndMS
			start := characterOffset
			end := start + utf8.RuneCountInString(text)
			idHash := sha256.Sum256([]byte(string(req.DraftID) + "\x00" + string(req.Input.SHA256) + "\x00transcript-segment\x00" + req.Format + "\x00" + string(req.Request.ProfileDigest) + "\x00" + converted.Version + "\x00" + strconv.Itoa(index) + "\x00" + text))
			blocks = append(blocks, contracts.ParsedBlock{ID: contracts.ID(hex.EncodeToString(idHash[:])), Order: index, Content: contracts.Content{Kind: contracts.TextBlock, Text: text, Source: contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: contracts.Locator{Precision: "transcript", Characters: &contracts.CharacterRange{Start: start, End: end}, StartMS: &startMS, EndMS: &endMS}}}})
			characterOffset = end + 2
			previousEnd = segment.EndMS
		}
		if strings.Join(joined, "\n\n") != converted.Text || len(blocks) == 0 {
			return contracts.ParseResult{}, ErrParse
		}
		return contracts.ParseResult{ParserVersion: converted.Version, Blocks: blocks, Diagnostics: []contracts.Diagnostic{{Code: "document_audio_transcript", Severity: "warning", SafeMessage: "音频转写文本按时间段绑定，字符范围对应转写结果而非音频字节"}}}, nil
	}
	hash := sha256.Sum256([]byte(string(req.DraftID) + "\x00" + string(req.Input.SHA256) + "\x00transcript\x00" + req.Format + "\x00" + string(req.Request.ProfileDigest) + "\x00" + converted.Version + "\x00" + converted.Text))
	return contracts.ParseResult{ParserVersion: converted.Version, Blocks: []contracts.ParsedBlock{{ID: contracts.ID(hex.EncodeToString(hash[:])), Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: converted.Text, Source: contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: contracts.Locator{Precision: "transcript", Characters: &contracts.CharacterRange{Start: 0, End: utf8.RuneCountInString(converted.Text)}}}}}}, Diagnostics: []contracts.Diagnostic{{Code: "document_audio_transcript", Severity: "warning", SafeMessage: "音频转写文本可检索，字符范围对应转写结果而非音频字节"}}}, nil
}
