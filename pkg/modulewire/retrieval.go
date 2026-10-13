package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

func EncodeRetrieveRequest(call Call, request contracts.RetrieveRequest) ([]byte, error) {
	if err := checkRetrieve(request); err != nil {
		return nil, err
	}
	input := retrieveInput{request.Query, request.Publication, request.Generations, request.BackendEpochID, request.RouteID, request.Channels, request.Limit}
	return encodeInvocation(call, request.Request, request.Scope, input)
}

func DecodeRetrieveRequest(data []byte) (Call, contracts.RetrieveRequest, error) {
	context, input, err := decodeInvocation[retrieveInput](data)
	if err != nil {
		return Call{}, contracts.RetrieveRequest{}, err
	}
	request := contracts.RetrieveRequest{Request: requestContext(context), Scope: context.Scope, Query: input.Query, Publication: input.Publication,
		Generations: input.Generations, BackendEpochID: input.BackendEpochID, RouteID: input.RouteID, Channels: input.Channels, Limit: input.Limit}
	if err := checkRetrieve(request); err != nil {
		return Call{}, contracts.RetrieveRequest{}, err
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, request, nil
}

func EncodeRetrieveResult(requestID, invocationID contracts.ID, candidates []contracts.Candidate) ([]byte, error) {
	return encodeResponse(requestID, invocationID, retrieveResult{candidates})
}

func DecodeRetrieveResult(data []byte, requestID, invocationID contracts.ID, expected contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	if err := checkRetrieve(expected); err != nil {
		return nil, err
	}
	result, err := decodeResponse[retrieveResult](data, requestID, invocationID)
	if err != nil {
		return nil, err
	}
	if len(result.Candidates) > expected.Limit {
		return nil, invalid()
	}
	for _, candidate := range result.Candidates {
		if err := checkCandidate(candidate, expected); err != nil {
			return nil, err
		}
	}
	return result.Candidates, nil
}
