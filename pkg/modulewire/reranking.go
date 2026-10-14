package modulewire

import (
	"reflect"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type rerankInput struct {
	Query      string
	Candidates []contracts.Candidate
	Limit      int
}
type rerankResult struct{ Evidence []contracts.Evidence }

func init() {
	register(invocation[rerankInput]{}, "Context:context Input:input")
	register(response[rerankResult]{}, "RequestID:request_id InvocationID:invocation_id Result:result")
	register(rerankInput{}, "Query:query Candidates:candidates Limit:limit")
	register(rerankResult{}, "Evidence:evidence")
	register(contracts.Evidence{}, "Citation:citation SourceExcerpt:source_excerpt Rank:rank")
}

func EncodeRerankRequest(call Call, scope contracts.AccessScope, request contracts.RerankRequest) ([]byte, error) {
	for _, candidate := range request.Candidates {
		if err := checkFusionOrigins(candidate); err != nil {
			return nil, err
		}
	}
	if request.Limit < 1 || request.Limit > 1000 {
		return nil, invalid()
	}
	return encodeInvocation(call, request.Request, scope, rerankInput{request.Query, request.Candidates, request.Limit})
}

func DecodeRerankRequest(data []byte) (Call, contracts.AccessScope, contracts.RerankRequest, error) {
	context, input, err := decodeInvocation[rerankInput](data)
	if err != nil {
		return Call{}, contracts.AccessScope{}, contracts.RerankRequest{}, err
	}
	if input.Limit < 1 || input.Limit > 1000 {
		return Call{}, contracts.AccessScope{}, contracts.RerankRequest{}, invalid()
	}
	for _, candidate := range input.Candidates {
		if err := checkFusionOrigins(candidate); err != nil {
			return Call{}, contracts.AccessScope{}, contracts.RerankRequest{}, err
		}
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, context.Scope,
		contracts.RerankRequest{Request: requestContext(context), Query: input.Query, Candidates: input.Candidates, Limit: input.Limit}, nil
}

func EncodeRerankResult(requestID, invocationID contracts.ID, evidence []contracts.Evidence) ([]byte, error) {
	return encodeResponse(requestID, invocationID, rerankResult{evidence})
}

func DecodeRerankResult(data []byte, requestID, invocationID contracts.ID, expected contracts.RerankRequest) ([]contracts.Evidence, error) {
	result, err := decodeResponse[rerankResult](data, requestID, invocationID)
	if err != nil {
		return nil, err
	}
	if len(result.Evidence) > expected.Limit || len(result.Evidence) > len(expected.Candidates) {
		return nil, invalid()
	}
	for _, evidence := range result.Evidence {
		found := false
		for _, candidate := range expected.Candidates {
			if reflect.DeepEqual(evidence.Citation, candidate.Citation) && strings.Contains(candidate.Text, evidence.SourceExcerpt) {
				found = true
			}
		}
		if !found || evidence.Rank < 1 {
			return nil, invalid()
		}
	}
	return result.Evidence, nil
}
