package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

func EncodeEmbedRequest(call Call, scope contracts.AccessScope, request contracts.EmbedRequest) ([]byte, error) {
	if request.Spec.Dimension < 1 || request.Spec.Dimension > 65536 || len(request.Texts) == 0 {
		return nil, invalid()
	}
	return encodeInvocation(call, request.Request, scope, embedInput{request.Texts, request.Spec})
}

func DecodeEmbedRequest(data []byte) (Call, contracts.AccessScope, contracts.EmbedRequest, error) {
	context, input, err := decodeInvocation[embedInput](data)
	if err != nil {
		return Call{}, contracts.AccessScope{}, contracts.EmbedRequest{}, err
	}
	if input.Spec.Dimension < 1 || input.Spec.Dimension > 65536 || len(input.Texts) == 0 {
		return Call{}, contracts.AccessScope{}, contracts.EmbedRequest{}, invalid()
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, context.Scope,
		contracts.EmbedRequest{Request: requestContext(context), Texts: input.Texts, Spec: input.Spec}, nil
}

func EncodeEmbedResult(requestID, invocationID contracts.ID, result contracts.EmbedResult) ([]byte, error) {
	return encodeResponse(requestID, invocationID, result)
}

func DecodeEmbedResult(data []byte, requestID, invocationID contracts.ID, expected contracts.EmbedRequest) (contracts.EmbedResult, error) {
	result, err := decodeResponse[contracts.EmbedResult](data, requestID, invocationID)
	if err != nil {
		return contracts.EmbedResult{}, err
	}
	if result.Spec != expected.Spec || len(result.Vectors) != len(expected.Texts) {
		return contracts.EmbedResult{}, invalid()
	}
	for _, vector := range result.Vectors {
		if len(vector) != expected.Spec.Dimension {
			return contracts.EmbedResult{}, invalid()
		}
	}
	return result, nil
}
