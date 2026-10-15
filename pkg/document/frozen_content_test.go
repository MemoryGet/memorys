package document_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func frozenDraftFixture() contracts.Draft {
	return contracts.Draft{ID: "draft", DocumentID: "document", State: "draft", RowVersion: 1, Blobs: []contracts.BlobRef{{ID: "original", SHA256: contracts.Digest(strings.Repeat("a", 64)), Size: 100, MediaType: "text/plain"}}, Blocks: []contracts.DraftBlock{{ID: "block", DraftID: "draft", RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: "中文😀\r\n保留空格  ", Source: contracts.SourceAnchor{Kind: "original", BlobID: "original", Locator: contracts.Locator{Precision: "coarse"}}}}}}
}

func TestFrozenMappingStablySortsAudioAndPreservesMediaSpelling(t *testing.T) {
	draft := frozenDraftFixture()
	blob := contracts.BlobRef{ID: "audio", SHA256: contracts.Digest(strings.Repeat("b", 64)), Size: 256, MediaType: "AuDiO/WaV"}
	draft.Blobs = append(draft.Blobs, blob)
	audio := &contracts.AudioContent{Blob: blob, DurationMS: 100, Segments: []contracts.AudioSegment{
		{StartMS: 50, EndMS: 70, Text: "later"},
		{StartMS: 10, EndMS: 20, Text: "tie-one"},
		{StartMS: 10, EndMS: 20, Text: "tie-two"},
	}}
	draft.Blocks[0].Content.Kind = contracts.AudioBlock
	draft.Blocks[0].Content.Audio = audio
	before, _ := json.Marshal(draft)
	result, err := document.BuildFrozenContent(draft)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(draft)
	if !bytes.Equal(before, after) || !bytes.Contains(result.Payload, []byte("AuDiO/WaV")) {
		t.Fatal("input mutated or MIME spelling changed")
	}
	first, second, last := bytes.Index(result.Payload, []byte("tie-one")), bytes.Index(result.Payload, []byte("tie-two")), bytes.Index(result.Payload, []byte("later"))
	if first < 0 || second <= first || last <= second {
		t.Fatal("audio order not stable")
	}
	audio.Segments[0].EndMS = 101
	if _, err := document.BuildFrozenContent(draft); err == nil {
		t.Fatal("segment beyond media duration accepted")
	}
}

func TestFrozenMappingExcludesMetadataAndReturnsOwnedBytes(t *testing.T) {
	draft := frozenDraftFixture()
	first, err := document.BuildFrozenContent(draft)
	if err != nil {
		t.Fatal(err)
	}
	draft.ID = "another-draft"
	draft.DocumentID = "another-document"
	draft.RowVersion = 9
	draft.Blocks[0].DraftID = draft.ID
	draft.Blocks[0].RowVersion = 8
	second, err := document.BuildFrozenContent(draft)
	if err != nil || first.ContentHash != second.ContentHash || !bytes.Equal(first.Payload, second.Payload) {
		t.Fatal("mutable metadata entered content identity", err)
	}
	first.Payload[0] = '!'
	if second.Payload[0] != '{' {
		t.Fatal("payloads aliased")
	}
	draft.Blocks[0].Content.Text += " "
	changed, err := document.BuildFrozenContent(draft)
	if err != nil || changed.ContentHash == second.ContentHash {
		t.Fatal("whitespace change was normalized", err)
	}
}

func TestFrozenMappingRejectsInvalidUnionSourceUTF8AndState(t *testing.T) {
	for _, name := range []string{"submitted", "foreign draft", "bad order", "foreign blob", "union", "invalid UTF8", "empty text", "bad hash"} {
		t.Run(name, func(t *testing.T) {
			draft := frozenDraftFixture()
			switch name {
			case "submitted":
				draft.State = "submitted"
			case "foreign draft":
				draft.Blocks[0].DraftID = "other"
			case "bad order":
				draft.Blocks[0].Order = 1
			case "foreign blob":
				draft.Blocks[0].Content.Source.BlobID = "other"
			case "union":
				draft.Blocks[0].Content.Table = &contracts.TableContent{}
			case "invalid UTF8":
				draft.Blocks[0].Content.Text = "\xff"
			case "empty text":
				draft.Blocks[0].Content.Text = " \r\n"
			case "bad hash":
				draft.Blobs[0].SHA256 = "bad"
			}
			if result, err := document.BuildFrozenContent(draft); err == nil || len(result.Payload) != 0 {
				t.Fatal("invalid content produced frozen identity")
			}
		})
	}
}

func TestFrozenMappingSortsSparseCellsWithoutMutatingInput(t *testing.T) {
	draft := frozenDraftFixture()
	content := &draft.Blocks[0].Content
	content.Kind = contracts.TableBlock
	content.Text = ""
	formula, number := "=A1+1", "0.00000000000000000001"
	content.Table = &contracts.TableContent{RowCount: 2, ColumnCount: 2, Cells: []contracts.TableCell{{Row: 1, Column: 1, RowSpan: 1, ColumnSpan: 1, Role: "data", Text: "", Formula: &formula, NumberText: &number}, {Row: 0, Column: 0, RowSpan: 1, ColumnSpan: 1, Role: "corner_header", Text: "标题"}}}
	before, _ := json.Marshal(draft)
	result, err := document.BuildFrozenContent(draft)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(draft)
	if !reflect.DeepEqual(before, after) || !bytes.Contains(result.Payload, []byte(number)) || !bytes.Contains(result.Payload, []byte(formula)) {
		t.Fatal("source table changed or exact annotations lost")
	}
	content.Table.Cells[0].Row = 0
	content.Table.Cells[0].Column = 1
	content.Table.Cells[1].ColumnSpan = 2
	if _, err := document.BuildFrozenContent(draft); err == nil {
		t.Fatal("overlapping merged cells accepted")
	}
}
