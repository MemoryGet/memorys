package chunk

import (
	"context"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestContentTextStructuredProjection(t *testing.T) {
	caption := "设备表"
	ocr := "型号 A"
	image := contracts.Content{Kind: contracts.ImageBlock, Image: &contracts.ImageContent{Caption: &caption, OCRText: &ocr}}
	if text, ok := ContentText(image); !ok || text != "设备表\n型号 A" {
		t.Fatalf("image projection: %q %v", text, ok)
	}
	table := contracts.Content{Kind: contracts.TableBlock, Table: &contracts.TableContent{RowCount: 2, ColumnCount: 2, Cells: []contracts.TableCell{{Row: 1, Column: 1, Text: "B"}, {Row: 0, Column: 0, Text: "A"}}}}
	if text, ok := ContentText(table); !ok || text != "A\nB" {
		t.Fatalf("table projection: %q %v", text, ok)
	}
	audio := contracts.Content{Kind: contracts.AudioBlock, Audio: &contracts.AudioContent{Segments: []contracts.AudioSegment{{Text: "第一段"}, {Text: "第二段"}}}}
	if text, ok := ContentText(audio); !ok || text != "第一段\n第二段" {
		t.Fatalf("audio projection: %q %v", text, ok)
	}
	if _, ok := ContentText(contracts.Content{Kind: contracts.ImageBlock, Image: &contracts.ImageContent{}}); ok {
		t.Fatal("image without caption/OCR became searchable")
	}
}

func TestSimpleSplitterIndexesStructuredContent(t *testing.T) {
	input := contracts.SplitRequest{Request: contracts.RequestContext{ProfileDigest: contracts.Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}, RevisionID: "revision", Policy: contracts.ChunkPolicy{Strategy: "characters", MaxCodepoints: 100, OverlapCodepoints: 0}, Blocks: []contracts.Block{{ID: "table", RevisionID: "revision", Content: contracts.Content{Kind: contracts.TableBlock, Table: &contracts.TableContent{RowCount: 1, ColumnCount: 1, Cells: []contracts.TableCell{{Row: 0, Column: 0, Text: "单元格"}}}}}}}
	chunks, err := (SimpleSplitter{}).Split(context.Background(), input)
	if err != nil || len(chunks) != 1 || chunks[0].Text != "单元格" || chunks[0].Spans[0].BlockID != "table" {
		t.Fatalf("structured split: %+v %v", chunks, err)
	}
}
