// Package parseassembly owns the executable host's parser implementation
// selection. Domain and HTTP packages receive only the contracts.Parser port.
package parseassembly

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
	"github.com/MemoryGet/memorys/pkg/localruntime"
	"github.com/MemoryGet/memorys/pkg/parser"
	"github.com/MemoryGet/memorys/pkg/parserprocess"
)

// Build creates the optional server parser runtime. Configuration is opt-in so
// a server without a durable parser environment never advertises queued tasks
// as processed by an invented fallback.
func Build(ctx context.Context, native *localruntime.Search) (*ingestion.ParseWorkerRuntime, error) {
	if native == nil || ctx == nil || strings.TrimSpace(os.Getenv("MEMORYS_PARSE_ENABLED")) != "1" {
		return nil, nil
	}
	global := strings.TrimSpace(os.Getenv("MEMORYS_PARSE_ALL")) == "1"
	space := contracts.ID(strings.TrimSpace(os.Getenv("MEMORYS_PARSE_SPACE_ID")))
	kb := contracts.ID(strings.TrimSpace(os.Getenv("MEMORYS_PARSE_KB_ID")))
	workerID := contracts.ID(strings.TrimSpace(os.Getenv("MEMORYS_PARSE_WORKER_ID")))
	if workerID == "" || !global && (space == "" || kb == "") {
		return nil, errors.New("parse worker partition configuration rejected")
	}
	store := native.ParserBlobStore()
	if store == nil {
		return nil, errors.New("parse worker blob store unavailable")
	}
	python, err := absoluteSetting("MEMORYS_PARSER_PYTHON", filepath.Join(".local", "markitdown-env", "Scripts", "python.exe"))
	if err != nil {
		return nil, errors.New("parse worker Python path rejected")
	}
	markScript, err := absoluteSetting("MEMORYS_PARSER_WORKER", filepath.Join("tools", "parsers", "markitdown_worker.py"))
	if err != nil {
		return nil, errors.New("parse worker Office script rejected")
	}
	pdfScript, err := absoluteSetting("MEMORYS_PDF_PARSER_WORKER", filepath.Join("tools", "parsers", "pypdf_worker.py"))
	if err != nil {
		return nil, errors.New("parse worker PDF script rejected")
	}
	ocrScript, err := absoluteSetting("MEMORYS_OCR_WORKER", filepath.Join("tools", "parsers", "ocr_worker.py"))
	if err != nil {
		return nil, errors.New("parse worker OCR script rejected")
	}
	transcriptScript, err := absoluteSetting("MEMORYS_TRANSCRIPTION_WORKER", filepath.Join("tools", "parsers", "transcribe_worker.py"))
	if err != nil {
		return nil, errors.New("parse worker transcription script rejected")
	}
	mark := parserprocess.MarkItDown{Python: python, Script: markScript}
	pdf := parserprocess.PyPDF{Python: python, Script: pdfScript}
	ocr := parserprocess.OCR{Python: python, Script: ocrScript}
	transcription := parserprocess.Transcription{Python: python, Script: transcriptScript}
	healthCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for name, health := range map[string]func(context.Context) error{
		"Office": mark.Health,
		"PDF":    pdf.Health,
		"OCR":    ocr.Health,
		"audio":  transcription.Health,
	} {
		if err := health(healthCtx); err != nil {
			return nil, errors.New("parse worker " + name + " parser unavailable")
		}
	}
	dispatch := parser.DispatchParser{Parsers: map[string]contracts.Parser{
		"markdown": parser.MarkdownParser{Store: store, MaxBytes: 8 << 20},
		"docx":     parser.OfficeParser{Store: store, Converter: mark, MaxBytes: 8 << 20},
		"xlsx":     parser.OfficeParser{Store: store, Converter: mark, MaxBytes: 8 << 20},
		"pptx":     parser.OfficeParser{Store: store, Converter: mark, MaxBytes: 8 << 20},
		"pdf":      parser.OfficeParser{Store: store, Converter: pdf, MaxBytes: 8 << 20},
		"png":      parser.OCRParser{Store: store, Converter: ocr, MaxBytes: 8 << 20},
		"jpg":      parser.OCRParser{Store: store, Converter: ocr, MaxBytes: 8 << 20},
		"wav":      parser.AudioParser{Store: store, Converter: transcription, MaxBytes: 8 << 20},
		"mp3":      parser.AudioParser{Store: store, Converter: transcription, MaxBytes: 8 << 20},
		"m4a":      parser.AudioParser{Store: store, Converter: transcription, MaxBytes: 8 << 20},
	}}
	onError := func(err error) {
		if err != nil {
			log.Printf("parse worker iteration failed: %v", err)
		}
	}
	if global {
		return native.NewParseWorkerAll(ctx, dispatch, workerID, 500*time.Millisecond, onError)
	}
	return native.NewParseWorker(ctx, dispatch, space, kb, workerID, 500*time.Millisecond, onError)
}

func absoluteSetting(name, fallback string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = fallback
	}
	return filepath.Abs(value)
}
