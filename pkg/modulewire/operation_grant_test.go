package modulewire_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func validateGrantFixtureOperation(operation string, raw []byte) error {
	var err error
	switch operation {
	case "parser_parse":
		_, _, err = modulewire.DecodeParseRequest(raw)
	case "embedder_embed":
		_, _, _, err = modulewire.DecodeEmbedRequest(raw)
	case "retriever_search", "composite_ingest_search_search":
		_, _, err = modulewire.DecodeRetrieveRequest(raw)
	case "reranker_rerank":
		_, _, _, err = modulewire.DecodeRerankRequest(raw)
	case "index_writer_prepare":
		_, _, err = modulewire.DecodeIndexPrepareRequest(raw)
	case "composite_ingest_search_prepare_projection":
		_, _, err = modulewire.DecodeCompositePrepareRequest(raw)
	case "index_writer_verify", "composite_ingest_search_verify":
		_, _, _, _, err = modulewire.DecodeIndexVerifyRequest(raw)
	case "index_writer_delete", "composite_ingest_search_delete":
		_, _, err = modulewire.DecodeIndexDeleteRequest(raw)
	case "blob_store_put":
		_, _, err = modulewire.DecodeBlobPutMetadata(raw)
	case "blob_store_open", "blob_store_delete":
		_, _, err = modulewire.DecodeBlobReadRequest(raw)
	case "generator_generate":
		_, _, err = modulewire.DecodeGenerateRequest(raw)
	case "control_status", "control_cancel":
		_, _, _, _, err = modulewire.DecodeControlRequest(raw)
	default:
		return fmt.Errorf("unknown grant fixture operation")
	}
	return err
}

func TestAllOperationGrantBindingsCrossLanguage(t *testing.T) {
	data, err := os.ReadFile("../../docs/architecture/examples/operation-grant-golden.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		Cases []struct {
			Operation     string `json:"operation"`
			Request       string `json:"request_json"`
			Scope         string `json:"scope_json"`
			Canonical     string `json:"canonical_request"`
			Digest        string `json:"request_digest"`
			ScopeDigest   string `json:"scope_digest"`
			Mutated       string `json:"mutated_request_json"`
			MutatedDigest string `json:"mutated_request_digest"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &suite); err != nil || len(suite.Cases) != 17 {
		t.Fatal("incomplete operation golden", err)
	}
	seen := map[string]bool{}
	for _, sample := range suite.Cases {
		if seen[sample.Operation] {
			t.Fatal("duplicate operation")
		}
		seen[sample.Operation] = true
		t.Run(sample.Operation, func(t *testing.T) {
			raw := []byte(sample.Request)
			if err := validateGrantFixtureOperation(sample.Operation, raw); err != nil {
				t.Fatal(err)
			}
			encoded, err := canonical.JSON(raw)
			if err != nil || string(encoded) != sample.Canonical {
				t.Fatal("canonical request mismatch", err)
			}
			digest, err := modulewire.GrantBindingDigest(raw, "request")
			if err != nil || string(digest) != sample.Digest {
				t.Fatal("request digest mismatch", err)
			}
			var envelope struct {
				Context struct {
					Scope json.RawMessage `json:"scope"`
				} `json:"context"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			scope, err := modulewire.GrantBindingDigest(envelope.Context.Scope, "scope")
			if err != nil || string(scope) != sample.ScopeDigest {
				t.Fatal("scope extraction mismatch", err)
			}
			separate, err := modulewire.GrantBindingDigest([]byte(sample.Scope), "scope")
			if err != nil || separate != scope {
				t.Fatal("independent scope mismatch", err)
			}
			mutated, err := modulewire.GrantBindingDigest([]byte(sample.Mutated), "request")
			if err != nil || string(mutated) != sample.MutatedDigest || mutated == digest {
				t.Fatal("input mutation not bound", err)
			}
			if err := validateGrantFixtureOperation(sample.Operation, []byte(sample.Mutated)); err == nil {
				t.Fatal("invalid input probe passed operation validation")
			}
		})
	}
}
