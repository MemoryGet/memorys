package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

func EncodeIndexVerifyRequest(call Call, request contracts.RequestContext, scope contracts.AccessScope, proof contracts.ReadinessProof) ([]byte, error) {
	if proof.GenerationSetID == "" || proof.AttemptID == "" || len(proof.Generations) == 0 {
		return nil, invalid()
	}
	return encodeInvocation(call, request, scope, verifyInput{proof})
}

func DecodeIndexVerifyRequest(data []byte) (Call, contracts.RequestContext, contracts.AccessScope, contracts.ReadinessProof, error) {
	context, input, err := decodeInvocation[verifyInput](data)
	if err != nil {
		return Call{}, contracts.RequestContext{}, contracts.AccessScope{}, contracts.ReadinessProof{}, err
	}
	if input.Proof.GenerationSetID == "" || input.Proof.AttemptID == "" || len(input.Proof.Generations) == 0 {
		return Call{}, contracts.RequestContext{}, contracts.AccessScope{}, contracts.ReadinessProof{}, invalid()
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, requestContext(context), context.Scope, input.Proof, nil
}

func EncodeIndexDeleteRequest(call Call, request contracts.IndexDeleteRequest) ([]byte, error) {
	if request.GenerationID == "" {
		return nil, invalid()
	}
	mode := "chunks"
	if len(request.ChunkIDs) == 0 {
		mode = "generation"
	}
	return encodeInvocation(call, request.Request, request.Scope, deleteInput{request.GenerationID, request.ChunkIDs, mode})
}

func DecodeIndexDeleteRequest(data []byte) (Call, contracts.IndexDeleteRequest, error) {
	context, input, err := decodeInvocation[deleteInput](data)
	if err != nil {
		return Call{}, contracts.IndexDeleteRequest{}, err
	}
	if input.GenerationID == "" || (input.Mode != "generation" && input.Mode != "chunks") ||
		(input.Mode == "generation" && len(input.ChunkIDs) != 0) || (input.Mode == "chunks" && len(input.ChunkIDs) == 0) {
		return Call{}, contracts.IndexDeleteRequest{}, invalid()
	}
	seen := make(map[contracts.ID]bool)
	for _, id := range input.ChunkIDs {
		if id == "" || seen[id] {
			return Call{}, contracts.IndexDeleteRequest{}, invalid()
		}
		seen[id] = true
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, contracts.IndexDeleteRequest{Request: requestContext(context), Scope: context.Scope, GenerationID: input.GenerationID, ChunkIDs: input.ChunkIDs}, nil
}

func EncodeIndexAck(requestID, invocationID contracts.ID) ([]byte, error) {
	return encodeResponse(requestID, invocationID, ack{true})
}

func DecodeIndexAck(data []byte, requestID, invocationID contracts.ID) error {
	value, err := decodeResponse[ack](data, requestID, invocationID)
	if err != nil {
		return err
	}
	if !value.Completed {
		return invalid()
	}
	return nil
}
