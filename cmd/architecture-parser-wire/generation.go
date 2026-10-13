package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

// inspectGeneration verifies event-data arrays, not actual HTTP/SSE framing or a model.
func inspectGeneration(output io.Writer, request, response, expectation json.RawMessage) error {
	call, value, err := modulewire.DecodeGenerateRequest(request)
	if err != nil {
		return err
	}
	var expected struct {
		ProfileVersion uint64 `json:"profile_version"`
		MaxEvents      int    `json:"max_events"`
		MaxTextBytes   int64  `json:"max_text_bytes"`
	}
	decoder := json.NewDecoder(bytes.NewReader(expectation))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&expected) != nil {
		return fmt.Errorf("invalid generation expectation")
	}
	citations := make([]contracts.Citation, len(value.Evidence))
	for i, evidence := range value.Evidence {
		citations[i] = evidence.Citation
	}
	sequence, err := modulewire.NewAnswerSequence(modulewire.GenerationExpectation{RequestID: value.Request.RequestID, ProfileVersion: expected.ProfileVersion, ProfileDigest: value.Request.ProfileDigest, Citations: citations}, modulewire.AnswerLimits{MaxEvents: expected.MaxEvents, MaxTextBytes: expected.MaxTextBytes})
	if err != nil {
		return err
	}
	var inputs []json.RawMessage
	if json.Unmarshal(response, &inputs) != nil {
		return fmt.Errorf("invalid generation event array")
	}
	events := make([]json.RawMessage, 0, len(inputs))
	for _, data := range inputs {
		event, err := modulewire.DecodeAnswerEvent(data, value.Request.RequestID)
		if err != nil {
			return err
		}
		if err := sequence.Accept(event); err != nil {
			return err
		}
		encoded, err := modulewire.EncodeAnswerEvent(event, value.Request.RequestID)
		if err != nil {
			return err
		}
		events = append(events, encoded)
	}
	outcome := "done"
	if err := sequence.Finish(); err != nil {
		var failure contracts.PublicError
		if !errors.As(err, &failure) {
			return err
		}
		outcome = "error"
	}
	encoded, err := modulewire.EncodeGenerateRequest(call, value)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(struct {
		Request  json.RawMessage   `json:"request"`
		Response []json.RawMessage `json:"response"`
		Outcome  string            `json:"stream_outcome"`
	}{encoded, events, outcome})
}
