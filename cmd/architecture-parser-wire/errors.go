package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func inspectError(output io.Writer, response, expectation json.RawMessage) error {
	var expected struct {
		RequestID    contracts.ID `json:"request_id"`
		InvocationID contracts.ID `json:"invocation_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(expectation))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&expected) != nil {
		return fmt.Errorf("invalid error expectation")
	}
	failure, err := modulewire.DecodeRemoteError(response, expected.RequestID, expected.InvocationID)
	if err != nil {
		return err
	}
	data, err := modulewire.EncodeRemoteError(expected.RequestID, expected.InvocationID, failure)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]json.RawMessage{"response": data})
}
