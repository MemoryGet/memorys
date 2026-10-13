package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	deadline := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	request := contracts.ParseRequest{Request: contracts.RequestContext{RequestID: "r", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Deadline: deadline},
		Scope:   contracts.AccessScope{ScopeID: "s", SubjectID: "u", SpaceID: "space", Action: "document.parse", PolicyVersion: 1, ExpiresAt: deadline},
		DraftID: "draft", Input: contracts.BlobRef{ID: "blob", SHA256: contracts.Digest(strings.Repeat("b", 64)), MediaType: "application/pdf"}, Format: "pdf"}
	requestBytes, err := modulewire.EncodeParseRequest(modulewire.Call{InvocationID: "i", ModuleInstanceID: "parser"}, request)
	if err != nil {
		t.Fatal(err)
	}
	responseBytes, err := modulewire.EncodeParseResult("r", "i", contracts.ParseResult{ParserVersion: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]json.RawMessage{"request": requestBytes, "response": responseBytes})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInspectionChecksEnvelopeAndResponseAssociation(t *testing.T) {
	data := fixture(t)
	var output bytes.Buffer
	if err := run(bytes.NewReader(data), &output); err != nil || !json.Valid(output.Bytes()) {
		t.Fatal("valid inspection failed", err)
	}
	for _, input := range []string{string(data) + `{}`, `{"request":{},"response":{},"extra":true}`, `{"request":{},"response":{}}`,
		strings.Replace(string(data), `"invocation_id":"i"`, `"invocation_id":"other"`, 1), strings.Repeat(" ", (4<<20)+1)} {
		if err := run(strings.NewReader(input), &bytes.Buffer{}); err == nil {
			t.Fatal("invalid inspection succeeded")
		}
	}
}
