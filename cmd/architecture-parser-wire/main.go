// This offline inspection tool does not execute parsers or authenticate grants.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func run(input io.Reader, output io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(input, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return fmt.Errorf("invalid inspection input")
	}
	if _, err := canonical.JSON(data); err != nil {
		return fmt.Errorf("invalid inspection JSON")
	}
	var fixture struct {
		Request, Response json.RawMessage
		Operation         string
		Expectation       json.RawMessage
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		return fmt.Errorf("invalid inspection envelope")
	}
	if fixture.Operation != "" {
		if fixture.Operation == "control_status" || fixture.Operation == "control_cancel" {
			return inspectControl(output, fixture.Operation, fixture.Request, fixture.Response, fixture.Expectation)
		}
		if fixture.Operation == "generator_generate" {
			return inspectGeneration(output, fixture.Request, fixture.Response, fixture.Expectation)
		}
		if fixture.Operation == "remote_error" {
			return inspectError(output, fixture.Response, fixture.Expectation)
		}
		if isBlobOperation(fixture.Operation) {
			return inspectBlob(output, fixture.Operation, fixture.Request, fixture.Response)
		}
		if isIndexOperation(fixture.Operation) {
			return inspectIndex(output, fixture.Operation, fixture.Request, fixture.Response, fixture.Expectation)
		}
		return inspectRetrieval(output, fixture.Operation, fixture.Request, fixture.Response)
	}
	call, request, err := modulewire.DecodeParseRequest(fixture.Request)
	if err != nil {
		return err
	}
	result, err := modulewire.DecodeParseResult(fixture.Response, request.Request.RequestID, call.InvocationID)
	if err != nil {
		return err
	}
	requestBytes, err := modulewire.EncodeParseRequest(call, request)
	if err != nil {
		return err
	}
	resultBytes, err := modulewire.EncodeParseResult(request.Request.RequestID, call.InvocationID, result)
	if err != nil {
		return err
	}
	resultEnvelope := struct {
		Request  json.RawMessage `json:"request"`
		Response json.RawMessage `json:"response"`
	}{requestBytes, resultBytes}
	return json.NewEncoder(output).Encode(resultEnvelope)
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "inspection_failed: "+err.Error())
		os.Exit(1)
	}
}
