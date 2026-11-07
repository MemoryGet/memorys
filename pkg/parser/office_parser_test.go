package parser

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type officeBlob struct {
	contracts.BlobStore
	raw []byte
}

func (b officeBlob) Open(context.Context, contracts.BlobReadRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b.raw))), nil
}

type convertedOffice struct{ calls int }

func (c *convertedOffice) Convert(context.Context, string, []byte) (MarkdownConversion, error) {
	c.calls++
	return MarkdownConversion{Text: "中文转换候选", Version: "markitdown/0.1.3"}, nil
}

type pagedPDFConverter struct{}

func (pagedPDFConverter) Convert(context.Context, string, []byte) (MarkdownConversion, error) {
	return MarkdownConversion{Text: "第一页\n\n第二页", Version: "pypdf/6.19.0", Pages: []string{"第一页", "第二页"}}, nil
}

type structuredOfficeConverter struct{}

func (structuredOfficeConverter) Convert(context.Context, string, []byte) (MarkdownConversion, error) {
	return MarkdownConversion{Text: "段落一\n\n段落二", Version: "markitdown/0.1.3", Blocks: []ConvertedBlock{{Text: "段落一", ParagraphPath: []int{1}}, {Text: "段落二", ParagraphPath: []int{3}}}}, nil
}

type structuredXLSXConverter struct{}

func (structuredXLSXConverter) Convert(context.Context, string, []byte) (MarkdownConversion, error) {
	return MarkdownConversion{Text: "一\n\n二\n\n三", Version: "markitdown/0.1.3", Blocks: []ConvertedBlock{{Text: "一", Worksheet: "Sheet1", CellRange: "A1"}, {Text: "二", Worksheet: "Sheet1", CellRange: "B1"}, {Text: "三", Worksheet: "Sheet1", CellRange: "A2"}}}, nil
}

type ocrConverterFixture struct{}

func (ocrConverterFixture) Convert(context.Context, string, []byte) (MarkdownConversion, error) {
	return MarkdownConversion{Text: "OCR text", Version: "ocr/tesseract-5", Pages: []string{"OCR text"}, OCRConfidence: []float64{91.25}}, nil
}

type transcriptionConverterFixture struct{}

func (transcriptionConverterFixture) Convert(context.Context, string, []byte) (MarkdownConversion, error) {
	return MarkdownConversion{Text: "转写结果", Version: "transcription/whisper-1"}, nil
}
func TestOfficeCandidateCoarseSourceAndInvalidSourceRejected(t *testing.T) {
	raw := []byte("fixture bytes")
	hash := sha256.Sum256(raw)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	r := contracts.ParseRequest{Format: "docx", DraftID: "draft", Input: contracts.BlobRef{ID: "blob", Size: int64(len(raw)), SHA256: contracts.Digest(fmt.Sprintf("%x", hash))}, Request: contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(fmt.Sprintf("%x", hash))}}
	c := &convertedOffice{}
	p := OfficeParser{Store: officeBlob{raw: raw}, Converter: c}
	result, err := p.Parse(ctx, r)
	if err != nil || len(result.Blocks) != 1 || result.Blocks[0].Content.Source.Locator.Precision != "coarse" || result.Blocks[0].Content.Source.Locator.Characters != nil || len(result.Diagnostics) == 0 {
		t.Fatal("converted text claimed exact source", err)
	}
	r.Input.SHA256 = contracts.Digest(strings.Repeat("0", 64))
	if _, err := p.Parse(ctx, r); err == nil || c.calls != 1 {
		t.Fatal("bad source reached converter")
	}
	r.Options.EnableOCR = true
	if _, err := p.Parse(ctx, r); err == nil || c.calls != 1 {
		t.Fatal("unsupported OCR silently accepted")
	}
}

func TestOfficeParserPreservesVerifiedPDFPageLocators(t *testing.T) {
	raw := []byte("pdf fixture")
	hash := sha256.Sum256(raw)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	r := contracts.ParseRequest{Format: "pdf", DraftID: "draft", Input: contracts.BlobRef{ID: "blob", Size: int64(len(raw)), SHA256: contracts.Digest(fmt.Sprintf("%x", hash))}, Request: contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(fmt.Sprintf("%x", hash))}}
	result, err := (OfficeParser{Store: officeBlob{raw: raw}, Converter: pagedPDFConverter{}}).Parse(ctx, r)
	if err != nil || len(result.Blocks) != 2 {
		t.Fatalf("paged PDF parse: %+v %v", result, err)
	}
	for index, block := range result.Blocks {
		locator := block.Content.Source.Locator
		if locator.Precision != "exact" || locator.Page == nil || *locator.Page != index+1 || locator.Characters == nil || locator.Characters.Start != 0 {
			t.Fatalf("page locator %d: %+v", index, locator)
		}
	}
}

func TestOfficeParserRejectsUnboundPDFPages(t *testing.T) {
	raw := []byte("pdf fixture")
	hash := sha256.Sum256(raw)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	r := contracts.ParseRequest{Format: "docx", DraftID: "draft", Input: contracts.BlobRef{ID: "blob", Size: int64(len(raw)), SHA256: contracts.Digest(fmt.Sprintf("%x", hash))}, Request: contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(fmt.Sprintf("%x", hash))}}
	converter := pagedPDFConverter{}
	if _, err := (OfficeParser{Store: officeBlob{raw: raw}, Converter: converter}).Parse(ctx, r); err == nil {
		t.Fatal("page metadata accepted for non-PDF format")
	}
}

func TestOfficeParserPreservesStructuredOfficeLocators(t *testing.T) {
	raw := []byte("docx fixture")
	hash := sha256.Sum256(raw)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	r := contracts.ParseRequest{Format: "docx", DraftID: "draft", Input: contracts.BlobRef{ID: "blob", Size: int64(len(raw)), SHA256: contracts.Digest(fmt.Sprintf("%x", hash))}, Request: contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(fmt.Sprintf("%x", hash))}}
	result, err := (OfficeParser{Store: officeBlob{raw: raw}, Converter: structuredOfficeConverter{}}).Parse(ctx, r)
	if err != nil || len(result.Blocks) != 2 {
		t.Fatalf("structured Office parse: %+v %v", result, err)
	}
	for index, block := range result.Blocks {
		locator := block.Content.Source.Locator
		if locator.Precision != "exact" || len(locator.ParagraphPath) != 1 || locator.Characters == nil || block.Order != index {
			t.Fatalf("structured locator %d: %+v", index, locator)
		}
	}
}

func TestOfficeParserAggregatesXLSXTableCells(t *testing.T) {
	raw := []byte("xlsx fixture")
	hash := sha256.Sum256(raw)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	r := contracts.ParseRequest{Format: "xlsx", DraftID: "draft", Input: contracts.BlobRef{ID: "blob", Size: int64(len(raw)), SHA256: contracts.Digest(fmt.Sprintf("%x", hash))}, Request: contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(fmt.Sprintf("%x", hash))}}
	result, err := (OfficeParser{Store: officeBlob{raw: raw}, Converter: structuredXLSXConverter{}}).Parse(ctx, r)
	if err != nil || len(result.Blocks) != 1 || result.Blocks[0].Content.Kind != contracts.TableBlock || result.Blocks[0].Content.Table == nil {
		t.Fatalf("structured XLSX table: %+v %v", result, err)
	}
	table := result.Blocks[0].Content.Table
	if table.RowCount != 2 || table.ColumnCount != 2 || len(table.Cells) != 3 || table.Cells[1].Source == nil || table.Cells[1].Source.Locator.CellRange != "B1" {
		t.Fatalf("structured XLSX cells: %+v", table)
	}
}

func TestOCRParserPreservesPageAndProjectionLocators(t *testing.T) {
	raw := []byte("png fixture")
	hash := sha256.Sum256(raw)
	profile := fmt.Sprintf("%x", hash)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	r := contracts.ParseRequest{Format: "png", DraftID: "draft", Input: contracts.BlobRef{ID: "blob", Size: int64(len(raw)), SHA256: contracts.Digest(fmt.Sprintf("%x", hash))}, Request: contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(profile)}, Options: contracts.ParseOptions{EnableOCR: true}}
	result, err := (OCRParser{Store: officeBlob{raw: raw}, Converter: ocrConverterFixture{}}).Parse(ctx, r)
	if err != nil || len(result.Blocks) != 1 {
		t.Fatalf("OCR parse: %+v %v", result, err)
	}
	locator := result.Blocks[0].Content.Source.Locator
	if locator.Precision != "ocr" || locator.Page == nil || *locator.Page != 1 || locator.Characters == nil || locator.Characters.End != len([]rune("OCR text")) || locator.Confidence == nil || *locator.Confidence != 91.25 {
		t.Fatalf("OCR locator: %+v", locator)
	}
}

func TestAudioParserBindsTranscriptToOriginal(t *testing.T) {
	raw := []byte("wav fixture")
	hash := sha256.Sum256(raw)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	r := contracts.ParseRequest{Format: "wav", DraftID: "draft", Input: contracts.BlobRef{ID: "blob", Size: int64(len(raw)), SHA256: contracts.Digest(fmt.Sprintf("%x", hash))}, Request: contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(fmt.Sprintf("%x", hash))}, Options: contracts.ParseOptions{EnableOCR: true}}
	result, err := (AudioParser{Store: officeBlob{raw: raw}, Converter: transcriptionConverterFixture{}}).Parse(ctx, r)
	if err != nil || len(result.Blocks) != 1 || result.Blocks[0].Content.Source.Locator.Precision != "transcript" {
		t.Fatalf("audio parse: %+v %v", result, err)
	}
}
