package adapters

import (
	"context"
	"mime"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contentverify"
	"github.com/MemoryGet/memorys/pkg/parser"
)

// officeOriginalText is an in-process strategy of the composite parser module.
// The trusted host supplies the converter, format allowlist and byte limit.
// No executable path or provider type enters the evidence verification port.
type officeOriginalText struct {
	converter parser.OfficeConverter
	formats   map[string]bool
	maxBytes  int64
}

func newOfficeOriginalText(converter parser.OfficeConverter, formats []string, maxBytes int64) (*officeOriginalText, error) {
	if missingPort(converter) || len(formats) == 0 || len(formats) > 4 || maxBytes < 1 || maxBytes > 8<<20 {
		return nil, ErrModule
	}
	allowed := map[string]bool{}
	for _, format := range formats {
		if allowed[format] || (format != "pdf" && format != "docx" && format != "xlsx" && format != "pptx") {
			return nil, ErrModule
		}
		allowed[format] = true
	}
	return &officeOriginalText{converter: converter, formats: allowed, maxBytes: maxBytes}, nil
}

func (p *officeOriginalText) ProjectOriginalText(ctx context.Context, media string, raw []byte) (contentverify.OriginalTextProjection, error) {
	if ctx == nil || p == nil || ctx.Err() != nil || len(raw) == 0 || int64(len(raw)) > p.maxBytes {
		return contentverify.OriginalTextProjection{}, ErrModule
	}
	if _, ok := ctx.Deadline(); !ok {
		return contentverify.OriginalTextProjection{}, ErrModule
	}
	kind, _, err := mime.ParseMediaType(media)
	if err != nil {
		return contentverify.OriginalTextProjection{}, contentverify.ErrUnsupportedEvidence
	}
	if kind == "text/plain" || kind == "text/markdown" {
		return (contentverify.UTF8OriginalText{}).ProjectOriginalText(ctx, media, raw)
	}
	format := map[string]string{"application/pdf": "pdf", "application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "xlsx", "application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx"}[kind]
	if format == "" || !p.formats[format] {
		return contentverify.OriginalTextProjection{}, contentverify.ErrUnsupportedEvidence
	}
	result, err := p.converter.Convert(ctx, format, append([]byte(nil), raw...))
	if err != nil || ctx.Err() != nil || result.Text == "" || len(result.Text) > 4<<20 || !utf8.ValidString(result.Text) || result.Version == "" || len(result.Version) > 200 || !utf8.ValidString(result.Version) {
		return contentverify.OriginalTextProjection{}, ErrModule
	}
	return contentverify.OriginalTextProjection{Text: result.Text, Version: result.Version}, nil
}

func (p *managedOffice) ProjectOriginalText(ctx context.Context, media string, raw []byte) (contentverify.OriginalTextProjection, error) {
	if p.check(ctx) != nil || p.original == nil {
		return contentverify.OriginalTextProjection{}, ErrModule
	}
	result, err := p.original.ProjectOriginalText(ctx, media, raw)
	if p.check(ctx) != nil {
		return contentverify.OriginalTextProjection{}, ErrModule
	}
	return result, err
}
