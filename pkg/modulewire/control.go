package modulewire

import (
	"bytes"
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type controlInput struct{ TargetInvocationID contracts.ID }
type InvocationStatus struct {
	TargetInvocationID contracts.ID
	State              string
	Result             *InvocationResult
	Error              *contracts.PublicError
}
type CancelResult struct {
	TargetInvocationID contracts.ID
	State              string
}

// ControlExpectation comes from the original trusted invocation ledger, not the response.
type ControlExpectation struct {
	TargetInvocationID   contracts.ID
	Operation            string
	TargetRequestID      contracts.ID
	TargetProfileDigest  contracts.Digest
	TargetProfileVersion uint64
}
type statusValue struct {
	TargetInvocationID contracts.ID    `json:"target_invocation_id"`
	State              string          `json:"state"`
	Result             json.RawMessage `json:"result,omitempty"`
	Error              json.RawMessage `json:"error,omitempty"`
}
type statusResponse struct {
	RequestID    contracts.ID `json:"request_id"`
	InvocationID contracts.ID `json:"invocation_id"`
	Result       statusValue  `json:"result"`
}

func init() {
	register(invocation[controlInput]{}, "Context:context Input:input")
	register(controlInput{}, "TargetInvocationID:target_invocation_id")
	register(response[CancelResult]{}, "RequestID:request_id InvocationID:invocation_id Result:result")
	register(CancelResult{}, "TargetInvocationID:target_invocation_id State:state")
}
func EncodeControlRequest(call Call, request contracts.RequestContext, scope contracts.AccessScope, target contracts.ID) ([]byte, error) {
	if target == "" || target == call.InvocationID {
		return nil, invalid()
	}
	return encodeInvocation(call, request, scope, controlInput{target})
}
func DecodeControlRequest(data []byte) (Call, contracts.RequestContext, contracts.AccessScope, contracts.ID, error) {
	context, input, err := decodeInvocation[controlInput](data)
	if err != nil {
		return Call{}, contracts.RequestContext{}, contracts.AccessScope{}, "", err
	}
	if input.TargetInvocationID == "" || input.TargetInvocationID == context.InvocationID {
		return Call{}, contracts.RequestContext{}, contracts.AccessScope{}, "", invalid()
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, requestContext(context), context.Scope, input.TargetInvocationID, nil
}

func checkStatus(status InvocationStatus, expected ControlExpectation) error {
	if ensureIDs(expected.TargetInvocationID, expected.TargetRequestID) != nil || !businessOperation(expected.Operation) || status.TargetInvocationID != expected.TargetInvocationID {
		return invalid()
	}
	switch status.State {
	case "succeeded":
		if status.Result == nil || status.Error != nil || status.Result.Operation != expected.Operation {
			return invalid()
		}
		if _, err := encodeInvocationResult(*status.Result); err != nil {
			return err
		}
		if status.Result.Answer != nil {
			answer := status.Result.Answer
			if !digestValue(expected.TargetProfileDigest) || expected.TargetProfileVersion == 0 || answer.RequestID != expected.TargetRequestID || answer.ProfileDigest != expected.TargetProfileDigest || answer.ProfileVersion != expected.TargetProfileVersion {
				return invalid()
			}
		}
	case "failed":
		if status.Result != nil || status.Error == nil {
			return invalid()
		}
		if _, err := NormalizeRemoteError(*status.Error, expected.TargetRequestID); err != nil {
			return err
		}
	case "running", "uncertain", "cancel_requested", "cancelled":
		if status.Result != nil || status.Error != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}

func businessOperation(operation string) bool {
	switch operation {
	case "parser_parse", "blob_store_put", "blob_store_open", "blob_store_delete", "embedder_embed", "index_writer_prepare", "index_writer_verify", "index_writer_delete", "retriever_search", "composite_ingest_search_prepare_projection", "composite_ingest_search_verify", "composite_ingest_search_delete", "composite_ingest_search_search", "reranker_rerank", "generator_generate":
		return true
	}
	return false
}
func EncodeInvocationStatus(requestID, invocationID contracts.ID, status InvocationStatus, expected ControlExpectation) ([]byte, error) {
	if ensureIDs(requestID, invocationID) != nil || expected.TargetInvocationID == invocationID {
		return nil, invalid()
	}
	if err := checkStatus(status, expected); err != nil {
		return nil, err
	}
	value := statusValue{TargetInvocationID: status.TargetInvocationID, State: status.State}
	if status.Result != nil {
		data, err := encodeInvocationResult(*status.Result)
		if err != nil {
			return nil, err
		}
		value.Result = data
	}
	if status.Error != nil {
		failure, err := NormalizeRemoteError(*status.Error, expected.TargetRequestID)
		if err != nil {
			return nil, err
		}
		data, err := encode(failure)
		if err != nil {
			return nil, err
		}
		value.Error = data
	}
	data, err := json.Marshal(statusResponse{requestID, invocationID, value})
	if err != nil {
		return nil, invalid()
	}
	if _, err := canonical.JSON(data); err != nil {
		return nil, invalid()
	}
	return data, nil
}
func DecodeInvocationStatus(data []byte, requestID, invocationID contracts.ID, expected ControlExpectation) (InvocationStatus, error) {
	if ensureIDs(requestID, invocationID) != nil || expected.TargetInvocationID == invocationID {
		return InvocationStatus{}, invalid()
	}
	if _, err := canonical.JSON(data); err != nil {
		return InvocationStatus{}, invalid()
	}
	var response statusResponse
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil || response.RequestID != requestID || response.InvocationID != invocationID {
		return InvocationStatus{}, invalid()
	}
	value := response.Result
	status := InvocationStatus{TargetInvocationID: value.TargetInvocationID, State: value.State}
	if value.Result != nil {
		result, err := decodeInvocationResult(value.Result)
		if err != nil {
			return status, err
		}
		status.Result = &result
	}
	if value.Error != nil {
		var failure contracts.PublicError
		if err := decode(value.Error, &failure); err != nil {
			return status, err
		}
		normalized, err := NormalizeRemoteError(failure, expected.TargetRequestID)
		if err != nil {
			return status, err
		}
		status.Error = &normalized
	}
	if err := checkStatus(status, expected); err != nil {
		return InvocationStatus{}, err
	}
	return status, nil
}
func EncodeCancelResult(requestID, invocationID contracts.ID, result CancelResult) ([]byte, error) {
	if result.TargetInvocationID == "" || result.TargetInvocationID == invocationID || (result.State != "cancel_requested" && result.State != "already_terminal") {
		return nil, invalid()
	}
	return encodeResponse(requestID, invocationID, result)
}
func DecodeCancelResult(data []byte, requestID, invocationID, target contracts.ID) (CancelResult, error) {
	result, err := decodeResponse[CancelResult](data, requestID, invocationID)
	if err != nil {
		return CancelResult{}, err
	}
	if target == "" || target == invocationID || result.TargetInvocationID != target || (result.State != "cancel_requested" && result.State != "already_terminal") {
		return CancelResult{}, invalid()
	}
	return result, nil
}
