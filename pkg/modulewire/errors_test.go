package modulewire_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func TestRemoteErrorProjectionDiscardsProviderText(t *testing.T) {
	for _, code := range []contracts.Code{"invalid_argument", "validation_failed", "unsupported_format", "unauthenticated", "forbidden", "not_found", "conflict", "publication_changed", "cursor_expired", "capability_missing", "contract_incompatible", "rate_limited", "deadline_exceeded", "dependency_unavailable", "internal"} {
		failure := contracts.PublicError{Code: code, SafeMessage: "SECRET token + private path + another space name", Retryable: true, RequestID: "r"}
		data, err := modulewire.EncodeRemoteError("r", "i", failure)
		if err != nil {
			t.Fatal(code, err)
		}
		if strings.Contains(string(data), "SECRET") || strings.Contains(string(data), "private path") {
			t.Fatal("unsafe text serialized", code)
		}
		actual, err := modulewire.DecodeRemoteError(data, "r", "i")
		if err != nil || actual.Code != code || actual.RequestID != "r" || actual.SafeMessage == failure.SafeMessage {
			t.Fatal("error identity lost", code, err)
		}
	}
}
func TestRemoteErrorInboundMessageNotTrustedAndRetryIsBounded(t *testing.T) {
	data := []byte(`{"request_id":"r","invocation_id":"i","error":{"code":"forbidden","safe_message":"SECRET: tenant exists","retryable":true,"request_id":"r"}}`)
	actual, err := modulewire.DecodeRemoteError(data, "r", "i")
	if err != nil || actual.SafeMessage != "Access denied" || actual.Retryable {
		t.Fatal("unsafe forbidden response", err)
	}
	data = []byte(strings.Replace(string(data), "forbidden", "dependency_unavailable", 1))
	actual, err = modulewire.DecodeRemoteError(data, "r", "i")
	if err != nil || actual.SafeMessage != "Dependency unavailable" || !actual.Retryable {
		t.Fatal("transient advisory lost", err)
	}
}
func TestRemoteErrorRejectsUnknownCodeRawCauseAndCorrelationMismatch(t *testing.T) {
	baseline := `{"request_id":"r","invocation_id":"i","error":{"code":"internal","safe_message":"Failure","retryable":false,"request_id":"r"}}`
	for _, data := range []string{strings.Replace(baseline, `"internal"`, `"vendor_failure"`, 1),
		strings.Replace(baseline, `"invocation_id":"i"`, `"invocation_id":"other"`, 1),
		strings.Replace(baseline, `"request_id":"r"`, `"request_id":"other"`, 1),
		strings.Replace(baseline, `"retryable":false`, `"retryable":false,"cause":"SECRET"`, 1)} {
		if _, err := modulewire.DecodeRemoteError([]byte(data), "r", "i"); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("invalid error accepted or cause exposed")
		}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(baseline), &payload); err != nil {
		t.Fatal(err)
	}
	payload["error"].(map[string]any)["request_id"] = "other"
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = modulewire.DecodeRemoteError(data, "r", "i"); err == nil {
		t.Fatal("inner request ID mismatch accepted")
	}
}
