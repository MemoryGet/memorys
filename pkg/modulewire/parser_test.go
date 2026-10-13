package modulewire_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func parseRequest() contracts.ParseRequest {
	deadline := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	return contracts.ParseRequest{Request: contracts.RequestContext{RequestID: "req-1", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Deadline: deadline},
		Scope:   contracts.AccessScope{ScopeID: "scope-1", SubjectID: "subject-1", Action: "document.parse", SpaceID: "space-1", KnowledgeBaseIDs: []contracts.ID{"kb-1"}, PolicyVersion: 9007199254740993, ExpiresAt: deadline},
		DraftID: "draft-1", Input: contracts.BlobRef{ID: "blob-1", SHA256: contracts.Digest(strings.Repeat("b", 64)), Size: 100, MediaType: "application/pdf"}, Format: "pdf",
		Options: contracts.ParseOptions{Languages: []string{"zh"}, EnableOCR: true}}
}

func TestParserRequestRoundTripKeepsExactScopeAndMetadata(t *testing.T) {
	request := parseRequest()
	call := modulewire.Call{InvocationID: "invoke-1", ModuleInstanceID: "parser-a"}
	encoded, err := modulewire.EncodeParseRequest(call, request)
	if err != nil {
		t.Fatal(err)
	}
	actualCall, actual, err := modulewire.DecodeParseRequest(encoded)
	if err != nil || actualCall != call || !reflect.DeepEqual(actual, request) {
		t.Fatal("request fields lost", err)
	}
	encoded = []byte(strings.Replace(string(encoded), `"size":100`, `"size":1e2`, 1))
	_, actual, err = modulewire.DecodeParseRequest(encoded)
	if err != nil || actual.Input.Size != 100 {
		t.Fatal("equivalent integer spelling rejected", err)
	}
}

func TestParserResponsePreservesTypedUnionAndSources(t *testing.T) {
	caption := ""
	formula, numberText, speaker := "=SUM(A1)", "0.00000001", "说话人甲"
	page, width, height := 1, 100, 50
	start, end := 0, 3
	source := contracts.SourceAnchor{Kind: "original", BlobID: "blob-1", Locator: contracts.Locator{Precision: "exact", Page: &page,
		Box: &contracts.BoundingBox{Left: 0.125, Top: 0, Right: 1, Bottom: 1}, ParagraphPath: []int{0, 1}, Characters: &contracts.CharacterRange{Start: start, End: end}},
		DerivedFrom: []contracts.OriginalAnchor{{BlobID: "blob-1", Locator: contracts.Locator{Precision: "coarse"}}}}
	result := contracts.ParseResult{ParserVersion: "fixture-1", Diagnostics: []contracts.Diagnostic{{Code: "ocr", SafeMessage: "核对", Severity: "warning"}}, Blocks: []contracts.ParsedBlock{
		{ID: "text", Order: 0, Content: contracts.Content{Kind: contracts.TextBlock, Text: "中文😀\r\n", Source: source}},
		{ID: "table", Order: 1, Content: contracts.Content{Kind: contracts.TableBlock, Text: "表", Source: source, Table: &contracts.TableContent{RowCount: 1, ColumnCount: 1, Caption: &caption, Cells: []contracts.TableCell{{RowSpan: 1, ColumnSpan: 1, Text: "0.00000001", Role: "data", Formula: &formula, NumberText: &numberText, Source: &source}}}}},
		{ID: "image", Order: 2, Content: contracts.Content{Kind: contracts.ImageBlock, Source: source, Image: &contracts.ImageContent{Blob: parseRequest().Input, Width: &width, Height: &height, Caption: &caption, OCRText: &numberText}}},
		{ID: "audio", Order: 3, Content: contracts.Content{Kind: contracts.AudioBlock, Source: source, Audio: &contracts.AudioContent{Blob: parseRequest().Input, DurationMS: 1000, Segments: []contracts.AudioSegment{{StartMS: 0, EndMS: 500, Text: "语音", SpeakerLabel: &speaker}}}}},
	}}
	encoded, err := modulewire.EncodeParseResult("req-1", "invoke-1", result)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := modulewire.DecodeParseResult(encoded, "req-1", "invoke-1")
	if err != nil || !reflect.DeepEqual(actual, result) {
		t.Fatal("content union or source changed", err)
	}
	if _, err := modulewire.DecodeParseResult(encoded, "other-request", "invoke-1"); err == nil {
		t.Fatal("response correlation not checked")
	}
	var object map[string]any
	if json.Unmarshal(encoded, &object) != nil {
		t.Fatal("invalid wire JSON")
	}
}
