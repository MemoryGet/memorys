package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func inspectControl(output io.Writer, operation string, request, response, expectation json.RawMessage) error {
	call, context, scope, target, err := modulewire.DecodeControlRequest(request)
	if err != nil {
		return err
	}
	encodedRequest, err := modulewire.EncodeControlRequest(call, context, scope, target)
	if err != nil {
		return err
	}
	var encodedResponse []byte
	if operation == "control_cancel" {
		result, err := modulewire.DecodeCancelResult(response, context.RequestID, call.InvocationID, target)
		if err != nil {
			return err
		}
		encodedResponse, err = modulewire.EncodeCancelResult(context.RequestID, call.InvocationID, result)
		if err != nil {
			return err
		}
	} else {
		var expected struct {
			Operation            string           `json:"operation"`
			TargetRequestID      contracts.ID     `json:"target_request_id"`
			TargetProfileDigest  contracts.Digest `json:"target_profile_digest"`
			TargetProfileVersion uint64           `json:"target_profile_version"`
		}
		decoder := json.NewDecoder(bytes.NewReader(expectation))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&expected) != nil {
			return fmt.Errorf("invalid control expectation")
		}
		proof := modulewire.ControlExpectation{TargetInvocationID: target, Operation: expected.Operation, TargetRequestID: expected.TargetRequestID, TargetProfileDigest: expected.TargetProfileDigest, TargetProfileVersion: expected.TargetProfileVersion}
		result, err := modulewire.DecodeInvocationStatus(response, context.RequestID, call.InvocationID, proof)
		if err != nil {
			return err
		}
		encodedResponse, err = modulewire.EncodeInvocationStatus(context.RequestID, call.InvocationID, result, proof)
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(output).Encode(map[string]json.RawMessage{"request": encodedRequest, "response": encodedResponse})
}
