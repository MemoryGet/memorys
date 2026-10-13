package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

func makeContext(call Call, request contracts.RequestContext, scope contracts.AccessScope) wireContext {
	return wireContext{RequestID: request.RequestID, InvocationID: call.InvocationID, ModuleInstanceID: call.ModuleInstanceID,
		ContractVersion: "1.0", Deadline: request.Deadline, ProfileDigest: request.ProfileDigest, Scope: scope}
}

func requestContext(value wireContext) contracts.RequestContext {
	return contracts.RequestContext{RequestID: value.RequestID, Deadline: value.Deadline, ProfileDigest: value.ProfileDigest}
}

func encodeInvocation[T any](call Call, request contracts.RequestContext, scope contracts.AccessScope, input T) ([]byte, error) {
	context := makeContext(call, request, scope)
	if err := checkContext(context); err != nil {
		return nil, err
	}
	return encode(invocation[T]{context, input})
}

func decodeInvocation[T any](data []byte) (wireContext, T, error) {
	var value invocation[T]
	if err := decode(data, &value); err != nil {
		return wireContext{}, value.Input, err
	}
	if err := checkContext(value.Context); err != nil {
		return wireContext{}, value.Input, err
	}
	return value.Context, value.Input, nil
}

func encodeResponse[T any](requestID, invocationID contracts.ID, value T) ([]byte, error) {
	if err := ensureIDs(requestID, invocationID); err != nil {
		return nil, err
	}
	return encode(response[T]{requestID, invocationID, value})
}

func decodeResponse[T any](data []byte, requestID, invocationID contracts.ID) (T, error) {
	var value response[T]
	if err := ensureIDs(requestID, invocationID); err != nil {
		return value.Result, err
	}
	if err := decode(data, &value); err != nil {
		return value.Result, err
	}
	if value.RequestID != requestID || value.InvocationID != invocationID {
		return value.Result, invalid()
	}
	return value.Result, nil
}
