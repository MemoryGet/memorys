package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

func EncodeIndexPrepareRequest(call Call, request contracts.IndexPrepareRequest) ([]byte, error) {
	if err := validateIndexInput(request); err != nil {
		return nil, err
	}
	input := indexInput{request.GenerationSetID, request.Generations, request.OperationID, request.AttemptID, request.FencingToken, request.Chunks, request.Vectors}
	return encodeInvocation(call, request.Request, request.Scope, input)
}

func DecodeIndexPrepareRequest(data []byte) (Call, contracts.IndexPrepareRequest, error) {
	context, input, err := decodeInvocation[indexInput](data)
	if err != nil {
		return Call{}, contracts.IndexPrepareRequest{}, err
	}
	request := contracts.IndexPrepareRequest{Request: requestContext(context), Scope: context.Scope, GenerationSetID: input.GenerationSetID,
		Generations: input.Generations, OperationID: input.OperationID, AttemptID: input.AttemptID, FencingToken: input.FencingToken, Chunks: input.Chunks, Vectors: input.Vectors}
	if err := validateIndexInput(request); err != nil {
		return Call{}, contracts.IndexPrepareRequest{}, err
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, request, nil
}

func EncodeCompositePrepareRequest(call Call, request contracts.CompositePrepareRequest) ([]byte, error) {
	if err := validateCompositeInput(request); err != nil {
		return nil, err
	}
	input := compositeInput{request.GenerationSetID, request.Generations, request.OperationID, request.AttemptID, request.FencingToken, request.Chunks, request.ProjectionFormatVersion}
	return encodeInvocation(call, request.Request, request.Scope, input)
}

func DecodeCompositePrepareRequest(data []byte) (Call, contracts.CompositePrepareRequest, error) {
	context, input, err := decodeInvocation[compositeInput](data)
	if err != nil {
		return Call{}, contracts.CompositePrepareRequest{}, err
	}
	request := contracts.CompositePrepareRequest{Request: requestContext(context), Scope: context.Scope, GenerationSetID: input.GenerationSetID,
		Generations: input.Generations, OperationID: input.OperationID, AttemptID: input.AttemptID, FencingToken: input.FencingToken, Chunks: input.Chunks, ProjectionFormatVersion: input.ProjectionFormatVersion}
	if err := validateCompositeInput(request); err != nil {
		return Call{}, contracts.CompositePrepareRequest{}, err
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, request, nil
}

func EncodePrepareResult(requestID, invocationID contracts.ID, result PrepareResult) ([]byte, error) {
	return encodeResponse(requestID, invocationID, result)
}

// expected comes from authority records, never from the remote result itself.
// A matching proof still needs live Verify and the platform publication CAS.
func DecodePrepareResult(data []byte, requestID, invocationID contracts.ID, expected PrepareExpectation) (PrepareResult, error) {
	result, err := decodeResponse[PrepareResult](data, requestID, invocationID)
	if err != nil {
		return PrepareResult{}, err
	}
	if err := validatePrepareResult(result, expected); err != nil {
		return PrepareResult{}, err
	}
	return result, nil
}
