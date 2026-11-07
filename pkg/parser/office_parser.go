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

// MarkdownConversion is the bounded text projection returned by an external
// converter. Pages is optional and is currently populated only by the pinned
// PDF converter; when present it must join exactly to Text so page citations
// cannot be inferred from an unverified projection.
type MarkdownConversion struct {
	Text, Version string
	Pages         []string
	Blocks        []ConvertedBlock
	AudioSegments []AudioSegment
	OCRConfidence []float64
}

type AudioSegment struct {
	StartMS int64
	EndMS   int64
	Text    string
}

type ConvertedBlock struct {
	Text          string `json:"text"`
	Page          *int   `json:"page,omitempty"`
	Slide         *int   `json:"slide,omitempty"`
	Worksheet     string `json:"worksheet,omitempty"`
	CellRange     string `json:"cell_range,omitempty"`
	ParagraphPath []int  `json:"paragraph_path,omitempty"`
}
type OfficeConverter interface {
	Convert(context.Context, string, []byte) (MarkdownConversion, error)
}
type OfficeParser struct {
	Store     contracts.BlobStore
	Converter OfficeConverter
	MaxBytes  int64
}

func ConvertedBlockID(draftID contracts.ID, inputDigest, format string, index int, text, profileDigest, version string) contracts.ID {
	seed := string(draftID) + "\x00" + string(inputDigest) + "\x00structured\x00" + format + "\x00" + strconv.Itoa(index) + "\x00" + text + "\x00" + string(profileDigest) + "\x00" + version
	hash := sha256.Sum256([]byte(seed))
	return contracts.ID(hex.EncodeToString(hash[:]))
}

func PageBlockID(draftID contracts.ID, inputDigest string, page int, text, profileDigest, version string) contracts.ID {
	seed := string(draftID) + "\x00" + inputDigest + "\x00pdf-page\x00" + strconv.Itoa(page) + "\x00" + text + "\x00" + profileDigest + "\x00" + version
	hash := sha256.Sum256([]byte(seed))
	return contracts.ID(hex.EncodeToString(hash[:]))
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
	if len(converted.Pages) > 0 {
		if req.Format != "pdf" || !pagesJoinExactly(converted.Text, converted.Pages) {
			return contracts.ParseResult{}, ErrParse
		}
		blocks := make([]contracts.ParsedBlock, 0, len(converted.Pages))
		for index, pageText := range converted.Pages {
			if pageText == "" {
				continue
			}
			page := index + 1
			blocks = append(blocks, contracts.ParsedBlock{ID: PageBlockID(req.DraftID, string(req.Input.SHA256), page, pageText, string(req.Request.ProfileDigest), converted.Version), Order: len(blocks), Content: contracts.Content{Kind: contracts.TextBlock, Text: pageText, Source: contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: contracts.Locator{Precision: "exact", Page: &page, Characters: &contracts.CharacterRange{Start: 0, End: utf8.RuneCountInString(pageText)}}}}})
		}
		if len(blocks) == 0 {
			return contracts.ParseResult{}, ErrParse
		}
		return contracts.ParseResult{ParserVersion: converted.Version, Blocks: blocks, Diagnostics: []contracts.Diagnostic{{Code: "document_projection_page_exact", Severity: "info", SafeMessage: "PDF text projection preserves page and character ranges"}}}, nil
	}
	if len(converted.Blocks) > 0 {
		if req.Format == "pdf" || !structuredJoinExactly(converted.Text, converted.Blocks) || len(converted.Blocks) > 10000 {
			return contracts.ParseResult{}, ErrParse
		}
		if req.Format == "xlsx" {
			blocks, err := xlsxTableBlocks(req, converted)
			if err != nil {
				return contracts.ParseResult{}, ErrParse
			}
			return contracts.ParseResult{ParserVersion: converted.Version, Blocks: blocks, Diagnostics: []contracts.Diagnostic{{Code: "document_projection_table_exact", Severity: "info", SafeMessage: "Excel 单元格按工作表聚合为结构化表格，并保留单元格来源定位"}}}, nil
		}
		blocks := make([]contracts.ParsedBlock, 0, len(converted.Blocks))
		for index, item := range converted.Blocks {
			if !validStructuredLocator(req.Format, item) || item.Text == "" {
				return contracts.ParseResult{}, ErrParse
			}
			locator := contracts.Locator{Precision: "exact", Characters: &contracts.CharacterRange{Start: 0, End: utf8.RuneCountInString(item.Text)}, ParagraphPath: append([]int(nil), item.ParagraphPath...), Worksheet: item.Worksheet, CellRange: item.CellRange}
			if item.Slide != nil {
				value := *item.Slide
				locator.Slide = &value
			}
			blocks = append(blocks, contracts.ParsedBlock{ID: ConvertedBlockID(req.DraftID, string(req.Input.SHA256), req.Format, index, item.Text, string(req.Request.ProfileDigest), converted.Version), Order: index, Content: contracts.Content{Kind: contracts.TextBlock, Text: item.Text, Source: contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: locator}}})
		}
		return contracts.ParseResult{ParserVersion: converted.Version, Blocks: blocks, Diagnostics: []contracts.Diagnostic{{Code: "document_projection_structured_exact", Severity: "info", SafeMessage: "Office text projection preserves paragraph, cell or slide locations"}}}, nil
	}
	id := sha256.Sum256([]byte(string(req.DraftID) + "\x00" + string(req.Input.SHA256) + "\x00" + req.Format + "\x00" + string(req.Request.ProfileDigest) + "\x00" + converted.Version))
	return contracts.ParseResult{ParserVersion: converted.Version, Blocks: []contracts.ParsedBlock{{ID: contracts.ID(hex.EncodeToString(id[:])), Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: converted.Text, Source: contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: contracts.Locator{Precision: "coarse"}}}}}, Diagnostics: []contracts.Diagnostic{{Code: "document_projection_coarse", Severity: "warning", SafeMessage: "Converted text is a candidate projection; exact pages, cells and structured tables are not available"}}}, nil
}

type xlsxTableGroup struct {
	Worksheet string
	Items     []ConvertedBlock
}

func xlsxTableBlocks(req contracts.ParseRequest, converted MarkdownConversion) ([]contracts.ParsedBlock, error) {
	groups := make([]xlsxTableGroup, 0)
	positions := map[string]int{}
	for _, item := range converted.Blocks {
		if item.Worksheet == "" || item.CellRange == "" || strings.TrimSpace(item.Text) == "" {
			return nil, ErrParse
		}
		index, ok := positions[item.Worksheet]
		if !ok {
			positions[item.Worksheet] = len(groups)
			groups = append(groups, xlsxTableGroup{Worksheet: item.Worksheet})
			index = len(groups) - 1
		}
		groups[index].Items = append(groups[index].Items, item)
	}
	blocks := make([]contracts.ParsedBlock, 0, len(groups))
	for groupIndex, group := range groups {
		cells := make([]contracts.TableCell, 0, len(group.Items))
		maxRow, maxColumn := 0, 0
		texts := make([]string, 0, len(group.Items))
		for _, item := range group.Items {
			row, column, err := excelCellPosition(item.CellRange)
			if err != nil {
				return nil, err
			}
			if row > maxRow {
				maxRow = row
			}
			if column > maxColumn {
				maxColumn = column
			}
			pageSource := contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: contracts.Locator{Precision: "exact", Worksheet: item.Worksheet, CellRange: item.CellRange, Characters: &contracts.CharacterRange{Start: 0, End: utf8.RuneCountInString(item.Text)}}}
			cells = append(cells, contracts.TableCell{Row: row - 1, Column: column - 1, RowSpan: 1, ColumnSpan: 1, Text: item.Text, Role: "data", Source: &pageSource})
			texts = append(texts, item.Text)
		}
		if len(cells) == 0 || maxRow < 1 || maxColumn < 1 {
			return nil, ErrParse
		}
		content := contracts.Content{Kind: contracts.TableBlock, Text: strings.Join(texts, "\n\n"), Source: contracts.SourceAnchor{Kind: "original", BlobID: req.Input.ID, Locator: contracts.Locator{Precision: "exact", Worksheet: group.Worksheet}}, Table: &contracts.TableContent{RowCount: maxRow, ColumnCount: maxColumn, Cells: cells}}
		id := ConvertedBlockID(req.DraftID, string(req.Input.SHA256), req.Format, groupIndex, content.Text, string(req.Request.ProfileDigest), converted.Version)
		blocks = append(blocks, contracts.ParsedBlock{ID: id, Order: groupIndex, Content: content})
	}
	return blocks, nil
}

func excelCellPosition(raw string) (int, int, error) {
	cell := strings.TrimSpace(strings.SplitN(raw, ":", 2)[0])
	if cell == "" || strings.ContainsAny(cell, "$ \t\r\n") {
		return 0, 0, ErrParse
	}
	index := 0
	for index < len(cell) && cell[index] >= 'A' && cell[index] <= 'Z' {
		index++
	}
	if index == 0 || index == len(cell) {
		return 0, 0, ErrParse
	}
	column := 0
	for _, value := range cell[:index] {
		column = column*26 + int(value-'A'+1)
	}
	row := 0
	for _, value := range cell[index:] {
		if value < '0' || value > '9' {
			return 0, 0, ErrParse
		}
		row = row*10 + int(value-'0')
	}
	if row < 1 || column < 1 {
		return 0, 0, ErrParse
	}
	return row, column, nil
}

func pagesJoinExactly(text string, pages []string) bool {
	if len(pages) == 0 || len(pages) > 200 {
		return false
	}
	var joined strings.Builder
	joined.Grow(len(text))
	for index, page := range pages {
		if index > 0 {
			joined.WriteString("\n\n")
		}
		joined.WriteString(page)
	}
	return joined.String() == text
}

func structuredJoinExactly(text string, blocks []ConvertedBlock) bool {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		parts = append(parts, block.Text)
	}
	return strings.Join(parts, "\n\n") == text
}

func validStructuredLocator(format string, block ConvertedBlock) bool {
	if block.Page != nil || (format == "docx" && (len(block.ParagraphPath) == 0 || block.Worksheet != "" || block.CellRange != "" || block.Slide != nil)) || (format == "xlsx" && (block.Worksheet == "" || block.CellRange == "" || len(block.ParagraphPath) != 0 || block.Slide != nil)) || (format == "pptx" && (block.Slide == nil || *block.Slide < 1 || len(block.ParagraphPath) != 0 || block.Worksheet != "" || block.CellRange != "")) {
		return false
	}
	if format == "docx" {
		for _, value := range block.ParagraphPath {
			if value < 0 {
				return false
			}
		}
		return true
	}
	return format == "xlsx" || format == "pptx"
}
