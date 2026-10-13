package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func inspectRetrieval(output io.Writer, operation string, request, response json.RawMessage) error {
	var encodedRequest, encodedResponse []byte
	var err error
	switch operation {
	case "embedder_embed":
		call, scope, value, failure := modulewire.DecodeEmbedRequest(request)
		if failure != nil {
			return failure
		}
		result, failure := modulewire.DecodeEmbedResult(response, value.Request.RequestID, call.InvocationID, value)
		if failure != nil {
			return failure
		}
		encodedRequest, err = modulewire.EncodeEmbedRequest(call, scope, value)
		if err != nil {
			return err
		}
		encodedResponse, err = modulewire.EncodeEmbedResult(value.Request.RequestID, call.InvocationID, result)
	case "retriever_search", "composite_ingest_search_search":
		call, value, failure := modulewire.DecodeRetrieveRequest(request)
		if failure != nil {
			return failure
		}
		result, failure := modulewire.DecodeRetrieveResult(response, value.Request.RequestID, call.InvocationID, value)
		if failure != nil {
			return failure
		}
		encodedRequest, err = modulewire.EncodeRetrieveRequest(call, value)
		if err != nil {
			return err
		}
		encodedResponse, err = modulewire.EncodeRetrieveResult(value.Request.RequestID, call.InvocationID, result)
	case "reranker_rerank":
		call, scope, value, failure := modulewire.DecodeRerankRequest(request)
		if failure != nil {
			return failure
		}
		result, failure := modulewire.DecodeRerankResult(response, value.Request.RequestID, call.InvocationID, value)
		if failure != nil {
			return failure
		}
		encodedRequest, err = modulewire.EncodeRerankRequest(call, scope, value)
		if err != nil {
			return err
		}
		encodedResponse, err = modulewire.EncodeRerankResult(value.Request.RequestID, call.InvocationID, result)
	default:
		return fmt.Errorf("unsupported inspection operation")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]json.RawMessage{"request": encodedRequest, "response": encodedResponse})
}
