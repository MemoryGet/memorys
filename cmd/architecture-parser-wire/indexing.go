package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func isIndexOperation(value string) bool {
	return strings.HasPrefix(value, "index_writer_") || value == "composite_ingest_search_prepare_projection" || value == "composite_ingest_search_verify" || value == "composite_ingest_search_delete"
}

func prepareExpected(data []byte, setID, attemptID contracts.ID, generations []contracts.IndexGeneration) (modulewire.PrepareExpectation, error) {
	var input struct {
		ContentDigest        contracts.Digest                  `json:"content_digest"`
		ChunkCounts          map[contracts.ID]int              `json:"chunk_counts"`
		EmbeddingSpecDigests map[contracts.ID]contracts.Digest `json:"embedding_spec_digests"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		return modulewire.PrepareExpectation{}, fmt.Errorf("invalid authoritative expectation fixture")
	}
	return modulewire.PrepareExpectation{GenerationSetID: setID, AttemptID: attemptID, Generations: generations, ContentDigest: input.ContentDigest,
		ChunkCounts: input.ChunkCounts, EmbeddingSpecDigests: input.EmbeddingSpecDigests}, nil
}

func inspectIndex(output io.Writer, operation string, request, response, expectation json.RawMessage) error {
	var encodedRequest, encodedResponse []byte
	var err error
	switch operation {
	case "index_writer_prepare", "composite_ingest_search_prepare_projection":
		encodedRequest, encodedResponse, err = inspectPreparation(operation, request, response, expectation)
	case "index_writer_verify", "composite_ingest_search_verify":
		call, context, scope, proof, failure := modulewire.DecodeIndexVerifyRequest(request)
		if failure != nil {
			return failure
		}
		if err := modulewire.DecodeIndexAck(response, context.RequestID, call.InvocationID); err != nil {
			return err
		}
		encodedRequest, err = modulewire.EncodeIndexVerifyRequest(call, context, scope, proof)
		if err != nil {
			return err
		}
		encodedResponse, err = modulewire.EncodeIndexAck(context.RequestID, call.InvocationID)
	case "index_writer_delete", "composite_ingest_search_delete":
		call, value, failure := modulewire.DecodeIndexDeleteRequest(request)
		if failure != nil {
			return failure
		}
		if err := modulewire.DecodeIndexAck(response, value.Request.RequestID, call.InvocationID); err != nil {
			return err
		}
		encodedRequest, err = modulewire.EncodeIndexDeleteRequest(call, value)
		if err != nil {
			return err
		}
		encodedResponse, err = modulewire.EncodeIndexAck(value.Request.RequestID, call.InvocationID)
	default:
		return fmt.Errorf("unsupported index inspection")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]json.RawMessage{"request": encodedRequest, "response": encodedResponse})
}

func inspectPreparation(operation string, request, response, expectation json.RawMessage) ([]byte, []byte, error) {
	var requestID, setID, attemptID contracts.ID
	var call modulewire.Call
	var generations []contracts.IndexGeneration
	var encodedRequest []byte
	var err error
	if operation == "index_writer_prepare" {
		var value contracts.IndexPrepareRequest
		call, value, err = modulewire.DecodeIndexPrepareRequest(request)
		if err != nil {
			return nil, nil, err
		}
		requestID, setID, attemptID, generations = value.Request.RequestID, value.GenerationSetID, value.AttemptID, value.Generations
		encodedRequest, err = modulewire.EncodeIndexPrepareRequest(call, value)
	} else {
		var value contracts.CompositePrepareRequest
		call, value, err = modulewire.DecodeCompositePrepareRequest(request)
		if err != nil {
			return nil, nil, err
		}
		requestID, setID, attemptID, generations = value.Request.RequestID, value.GenerationSetID, value.AttemptID, value.Generations
		encodedRequest, err = modulewire.EncodeCompositePrepareRequest(call, value)
	}
	if err != nil {
		return nil, nil, err
	}
	expected, err := prepareExpected(expectation, setID, attemptID, generations)
	if err != nil {
		return nil, nil, err
	}
	result, err := modulewire.DecodePrepareResult(response, requestID, call.InvocationID, expected)
	if err != nil {
		return nil, nil, err
	}
	encodedResponse, err := modulewire.EncodePrepareResult(requestID, call.InvocationID, result)
	return encodedRequest, encodedResponse, err
}
