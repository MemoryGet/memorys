package parserprocess

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/parser"
)

// PyPDF is an independently pinned PDF-only converter, not an OCR backend.
type PyPDF struct{ Python, Script string }

func (p PyPDF) process() workerProcess {
	return workerProcess{p.Python, p.Script, "pypdf/6.19.0", []string{"pdf"}}
}
func (p PyPDF) Health(ctx context.Context) error { return p.process().Health(ctx) }
func (p PyPDF) Convert(ctx context.Context, format string, raw []byte) (parser.MarkdownConversion, error) {
	return p.process().Convert(ctx, format, raw)
}
