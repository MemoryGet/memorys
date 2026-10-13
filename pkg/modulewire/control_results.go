package modulewire

import (
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// InvocationResult is a closed, provider-neutral data union. It is not a readiness
// or authorization proof; results still need their original operation validators.
type InvocationResult struct {
	Operation  string
	Parsed     *contracts.ParseResult
	Blob       *contracts.BlobRef
	Embeddings *contracts.EmbedResult
	Prepared   *PrepareResult
	Candidates *[]contracts.Candidate
	Evidence   *[]contracts.Evidence
	Answer     *contracts.AnswerResult
	Completed  *bool
}
type invocationValue[T any] struct {
	Operation string
	Value     T
}
type blobTransferSummary struct{ Blob contracts.BlobRef }

func init() {
	register(invocationValue[contracts.ParseResult]{}, "Operation:operation Value:value")
	register(invocationValue[contracts.BlobRef]{}, "Operation:operation Value:value")
	register(invocationValue[blobTransferSummary]{}, "Operation:operation Value:value")
	register(blobTransferSummary{}, "Blob:blob")
	register(invocationValue[contracts.EmbedResult]{}, "Operation:operation Value:value")
	register(invocationValue[PrepareResult]{}, "Operation:operation Value:value")
	register(invocationValue[retrieveResult]{}, "Operation:operation Value:value")
	register(invocationValue[rerankResult]{}, "Operation:operation Value:value")
	register(invocationValue[contracts.AnswerResult]{}, "Operation:operation Value:value")
	register(invocationValue[ack]{}, "Operation:operation Value:value")
}

func encodeInvocationResult(result InvocationResult) ([]byte, error) {
	count := 0
	for _, present := range []bool{result.Parsed != nil, result.Blob != nil, result.Embeddings != nil, result.Prepared != nil, result.Candidates != nil, result.Evidence != nil, result.Answer != nil, result.Completed != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return nil, invalid()
	}
	switch result.Operation {
	case "parser_parse":
		if result.Parsed != nil {
			return encode(invocationValue[contracts.ParseResult]{result.Operation, *result.Parsed})
		}
	case "blob_store_put":
		if result.Blob != nil && validBlob(*result.Blob) {
			return encode(invocationValue[contracts.BlobRef]{result.Operation, *result.Blob})
		}
	case "blob_store_open":
		if result.Blob != nil && validBlob(*result.Blob) {
			return encode(invocationValue[blobTransferSummary]{result.Operation, blobTransferSummary{*result.Blob}})
		}
	case "embedder_embed":
		if result.Embeddings != nil {
			return encode(invocationValue[contracts.EmbedResult]{result.Operation, *result.Embeddings})
		}
	case "index_writer_prepare", "composite_ingest_search_prepare_projection":
		if result.Prepared != nil {
			return encode(invocationValue[PrepareResult]{result.Operation, *result.Prepared})
		}
	case "retriever_search", "composite_ingest_search_search":
		if result.Candidates != nil {
			return encode(invocationValue[retrieveResult]{result.Operation, retrieveResult{*result.Candidates}})
		}
	case "reranker_rerank":
		if result.Evidence != nil {
			return encode(invocationValue[rerankResult]{result.Operation, rerankResult{*result.Evidence}})
		}
	case "generator_generate":
		if result.Answer != nil {
			if err := checkAnswerEvent(contracts.AnswerEvent{Kind: "done", Result: result.Answer}, result.Answer.RequestID); err != nil {
				return nil, err
			}
			return encode(invocationValue[contracts.AnswerResult]{result.Operation, *result.Answer})
		}
	case "blob_store_delete", "index_writer_verify", "index_writer_delete", "composite_ingest_search_verify", "composite_ingest_search_delete":
		if result.Completed != nil && *result.Completed {
			return encode(invocationValue[ack]{result.Operation, ack{true}})
		}
	}
	return nil, invalid()
}

func decodeInvocationValue[T any](data []byte) (T, error) {
	var value invocationValue[T]
	err := decode(data, &value)
	return value.Value, err
}
func decodeInvocationResult(data []byte) (InvocationResult, error) {
	if len(data) > 4<<20 {
		return InvocationResult{}, invalid()
	}
	var tag struct {
		Operation string `json:"operation"`
	}
	if json.Unmarshal(data, &tag) != nil {
		return InvocationResult{}, invalid()
	}
	result := InvocationResult{Operation: tag.Operation}
	switch tag.Operation {
	case "parser_parse":
		value, err := decodeInvocationValue[contracts.ParseResult](data)
		if err != nil {
			return result, err
		}
		result.Parsed = &value
	case "blob_store_put":
		value, err := decodeInvocationValue[contracts.BlobRef](data)
		if err != nil {
			return result, err
		}
		result.Blob = &value
	case "blob_store_open":
		value, err := decodeInvocationValue[blobTransferSummary](data)
		if err != nil {
			return result, err
		}
		result.Blob = &value.Blob
	case "embedder_embed":
		value, err := decodeInvocationValue[contracts.EmbedResult](data)
		if err != nil {
			return result, err
		}
		result.Embeddings = &value
	case "index_writer_prepare", "composite_ingest_search_prepare_projection":
		value, err := decodeInvocationValue[PrepareResult](data)
		if err != nil {
			return result, err
		}
		result.Prepared = &value
	case "retriever_search", "composite_ingest_search_search":
		value, err := decodeInvocationValue[retrieveResult](data)
		if err != nil {
			return result, err
		}
		result.Candidates = &value.Candidates
	case "reranker_rerank":
		value, err := decodeInvocationValue[rerankResult](data)
		if err != nil {
			return result, err
		}
		result.Evidence = &value.Evidence
	case "generator_generate":
		value, err := decodeInvocationValue[contracts.AnswerResult](data)
		if err != nil {
			return result, err
		}
		result.Answer = &value
	case "blob_store_delete", "index_writer_verify", "index_writer_delete", "composite_ingest_search_verify", "composite_ingest_search_delete":
		value, err := decodeInvocationValue[ack](data)
		if err != nil {
			return result, err
		}
		result.Completed = &value.Completed
	default:
		return result, invalid()
	}
	if _, err := encodeInvocationResult(result); err != nil {
		return InvocationResult{}, err
	}
	return result, nil
}
