package modulewire_test

import (
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func TestStrictRequestDecodingRejectsLossyAndAmbiguousValues(t *testing.T) {
	request := parseRequest()
	encoded, err := modulewire.EncodeParseRequest(modulewire.Call{InvocationID: "invoke-1", ModuleInstanceID: "parser-a"}, request)
	if err != nil {
		t.Fatal(err)
	}
	input := string(encoded)
	for _, invalid := range []string{
		strings.Replace(input, `"size":100`, `"size":100.5`, 1),
		strings.Replace(input, `"size":100`, `"size":1e10000`, 1),
		strings.Replace(input, `"size":100`, `"size":9223372036854775808`, 1),
		strings.Replace(input, `"policy_version":9007199254740993`, `"policy_version":18446744073709551616`, 1),
		strings.Replace(input, `"enable_ocr":true`, `"enable_ocr":null`, 1),
		strings.Replace(input, `"enable_ocr":true`, `"enable_ocr":true,"extra":"private-value"`, 1),
		strings.Replace(input, `"enable_ocr":true`, `"enable_ocr":true,"enable_ocr":false`, 1),
		strings.Replace(input, `"contract_version":"1.0"`, `"contract_version":"2.0"`, 1),
		strings.Replace(input, `"deadline":"2026-10-12T12:00:00Z"`, `"deadline":"2026-10-13T12:00:00Z"`, 1),
		input + `{}`, `{"input":{}}`,
	} {
		if invalid == input {
			t.Fatal("invalid test did not mutate request")
		}
		if _, _, err := modulewire.DecodeParseRequest([]byte(invalid)); err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatal("accepted invalid or unsafe request")
		}
	}
}

func TestContentShapeRejectsNativeFieldsAndWrongDiscriminators(t *testing.T) {
	result := contracts.ParseResult{ParserVersion: "fixture", Blocks: []contracts.ParsedBlock{{ID: "b", Content: contracts.Content{
		Kind: contracts.TextBlock, Text: "文", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "b", Locator: contracts.Locator{Precision: "coarse"}}}}}}
	encoded, err := modulewire.EncodeParseResult("r", "i", result)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(string) string{
		func(s string) string { return strings.Replace(s, `"kind":"text"`, `"kind":"unknown"`, 1) },
		func(s string) string {
			return strings.Replace(s, `"kind":"text"`, `"kind":"text","structured_content":{}`, 1)
		},
		func(s string) string {
			return strings.Replace(s, `"kind":"text"`, `"kind":"table","structured_content":{"type":"image"}`, 1)
		},
		func(s string) string { return strings.Replace(s, `"kind":"text"`, `"kind":"text","content":{}`, 1) },
		func(s string) string {
			return strings.Replace(s, `"precision":"coarse"`, `"precision":"coarse","char_start":0`, 1)
		},
		func(s string) string {
			return strings.Replace(s, `"precision":"coarse"`, `"precision":"coarse","box":{}`, 1)
		},
		func(s string) string {
			return strings.Replace(s, `"precision":"coarse"`, `"precision":"coarse","bbox":[0,1,2]`, 1)
		},
	} {
		if _, err := modulewire.DecodeParseResult([]byte(mutate(string(encoded))), "r", "i"); err == nil {
			t.Fatal("invalid content accepted")
		}
	}
	result.Blocks[0].Content.Image = &contracts.ImageContent{}
	if _, err := modulewire.EncodeParseResult("r", "i", result); err == nil {
		t.Fatal("mixed union accepted")
	}
}

func TestEncodingDoesNotReplaceInvalidUTF8(t *testing.T) {
	request := parseRequest()
	request.Format = string([]byte{255})
	if _, err := modulewire.EncodeParseRequest(modulewire.Call{InvocationID: "i", ModuleInstanceID: "m"}, request); err == nil {
		t.Fatal("invalid UTF-8 was silently replaced")
	}
}
