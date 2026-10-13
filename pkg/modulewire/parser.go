package modulewire

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// EncodeParseRequest serializes data only. The caller must validate authorization
// and obtain the access grant separately; credentials are never encoded here.
func EncodeParseRequest(call Call, request contracts.ParseRequest) ([]byte, error) {
	invocation := parseInvocation{Context: wireContext{RequestID: request.Request.RequestID, InvocationID: call.InvocationID,
		ModuleInstanceID: call.ModuleInstanceID, ContractVersion: "1.0", Deadline: request.Request.Deadline,
		ProfileDigest: request.Request.ProfileDigest, Scope: request.Scope},
		Input: parseInput{request.DraftID, request.Input, request.Format, request.Options}}
	if err := checkContext(invocation.Context); err != nil {
		return nil, err
	}
	return encode(invocation)
}

func DecodeParseRequest(data []byte) (Call, contracts.ParseRequest, error) {
	var invocation parseInvocation
	if err := decode(data, &invocation); err != nil {
		return Call{}, contracts.ParseRequest{}, err
	}
	context := invocation.Context
	if err := checkContext(context); err != nil {
		return Call{}, contracts.ParseRequest{}, err
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, contracts.ParseRequest{
		Request: contracts.RequestContext{RequestID: context.RequestID, ProfileDigest: context.ProfileDigest, Deadline: context.Deadline},
		Scope:   context.Scope, DraftID: invocation.Input.DraftID, Input: invocation.Input.Input, Format: invocation.Input.Format, Options: invocation.Input.Options}, nil
}

func EncodeParseResult(requestID, invocationID contracts.ID, result contracts.ParseResult) ([]byte, error) {
	if err := ensureIDs(requestID, invocationID); err != nil {
		return nil, err
	}
	return encode(parseResponse{requestID, invocationID, result})
}

func DecodeParseResult(data []byte, requestID, invocationID contracts.ID) (contracts.ParseResult, error) {
	if err := ensureIDs(requestID, invocationID); err != nil {
		return contracts.ParseResult{}, err
	}
	var response parseResponse
	if err := decode(data, &response); err != nil {
		return contracts.ParseResult{}, err
	}
	if response.RequestID != requestID || response.InvocationID != invocationID {
		return contracts.ParseResult{}, invalid()
	}
	return response.Result, nil
}

func checkContext(context wireContext) error {
	if ensureIDs(context.RequestID, context.InvocationID, context.ModuleInstanceID, context.Scope.ScopeID,
		context.Scope.SubjectID, context.Scope.SpaceID) != nil || context.ContractVersion != "1.0" || context.Scope.Action == "" ||
		context.Scope.PolicyVersion == 0 || context.Deadline.IsZero() || context.Scope.ExpiresAt.IsZero() ||
		context.Deadline.After(context.Scope.ExpiresAt) {
		return invalid()
	}
	if len(context.ProfileDigest) != 64 {
		return invalid()
	}
	for _, char := range context.ProfileDigest {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return invalid()
		}
	}
	return nil
}
