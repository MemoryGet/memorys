package modulewire_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func controlExpected(operation string) modulewire.ControlExpectation {
	return modulewire.ControlExpectation{TargetInvocationID: "original-invocation", Operation: operation, TargetRequestID: "original-request"}
}
func TestControlRequestAndCancelRoundTrip(t *testing.T) {
	base := parseRequest()
	call := modulewire.Call{InvocationID: "control-invocation", ModuleInstanceID: "module-a"}
	data, err := modulewire.EncodeControlRequest(call, base.Request, base.Scope, "original-invocation")
	if err != nil {
		t.Fatal(err)
	}
	gotCall, request, scope, target, err := modulewire.DecodeControlRequest(data)
	if err != nil || gotCall != call || !reflect.DeepEqual(request, base.Request) || !reflect.DeepEqual(scope, base.Scope) || target != "original-invocation" {
		t.Fatal("control request fields lost", err)
	}
	if _, err := modulewire.EncodeControlRequest(call, base.Request, base.Scope, call.InvocationID); err == nil {
		t.Fatal("self-control accepted")
	}
	for _, state := range []string{"cancel_requested", "already_terminal"} {
		result := modulewire.CancelResult{TargetInvocationID: target, State: state}
		data, err := modulewire.EncodeCancelResult("control-request", call.InvocationID, result)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := modulewire.DecodeCancelResult(data, "control-request", call.InvocationID, target)
		if err != nil || actual != result {
			t.Fatal("cancel result changed", err)
		}
		if _, err := modulewire.DecodeCancelResult(data, "control-request", call.InvocationID, "foreign-target"); err == nil {
			t.Fatal("foreign cancel target accepted")
		}
	}
	if _, err := modulewire.EncodeCancelResult("control-request", call.InvocationID, modulewire.CancelResult{TargetInvocationID: target, State: "cancelled"}); err == nil {
		t.Fatal("request claimed confirmed cancellation")
	}
}
func TestStatusSixStatesAndOriginalErrorIdentity(t *testing.T) {
	expected := controlExpected("blob_store_delete")
	completed := true
	for _, state := range []string{"running", "uncertain", "cancel_requested", "cancelled", "succeeded", "failed"} {
		status := modulewire.InvocationStatus{TargetInvocationID: expected.TargetInvocationID, State: state}
		if state == "succeeded" {
			status.Result = &modulewire.InvocationResult{Operation: expected.Operation, Completed: &completed}
		}
		if state == "failed" {
			status.Error = &contracts.PublicError{Code: "internal", SafeMessage: "SECRET", Retryable: true, RequestID: expected.TargetRequestID}
		}
		data, err := modulewire.EncodeInvocationStatus("control-request", "control-invocation", status, expected)
		if err != nil {
			t.Fatal(state, err)
		}
		if strings.Contains(string(data), "SECRET") {
			t.Fatal("status leaked original failure")
		}
		actual, err := modulewire.DecodeInvocationStatus(data, "control-request", "control-invocation", expected)
		if err != nil || actual.State != state || actual.TargetInvocationID != expected.TargetInvocationID {
			t.Fatal("status fields lost", state, err)
		}
		if state == "failed" && (actual.Error.RequestID != expected.TargetRequestID || actual.Error.SafeMessage != "Internal failure" || actual.Error.Retryable) {
			t.Fatal("original/control request identity confused")
		}
	}
}
func TestStatusRejectsIllegalPayloadsWrongOperationAndFailedAck(t *testing.T) {
	expected := controlExpected("blob_store_delete")
	completed := true
	success := &modulewire.InvocationResult{Operation: expected.Operation, Completed: &completed}
	for _, status := range []modulewire.InvocationStatus{{TargetInvocationID: expected.TargetInvocationID, State: "running", Result: success},
		{TargetInvocationID: expected.TargetInvocationID, State: "succeeded"}, {TargetInvocationID: expected.TargetInvocationID, State: "failed"},
		{TargetInvocationID: expected.TargetInvocationID, State: "unknown"}, {TargetInvocationID: "other", State: "running"}} {
		if _, err := modulewire.EncodeInvocationStatus("control-request", "control-invocation", status, expected); err == nil {
			t.Fatal("illegal status accepted")
		}
	}
	wrong := modulewire.InvocationStatus{TargetInvocationID: expected.TargetInvocationID, State: "succeeded", Result: &modulewire.InvocationResult{Operation: "index_writer_delete", Completed: &completed}}
	if _, err := modulewire.EncodeInvocationStatus("control-request", "control-invocation", wrong, expected); err == nil {
		t.Fatal("operation substitution accepted")
	}
	completed = false
	wrong.Result.Operation = expected.Operation
	if _, err := modulewire.EncodeInvocationStatus("control-request", "control-invocation", wrong, expected); err == nil {
		t.Fatal("completed=false succeeded")
	}
	wrong.State = "running"
	wrong.Result = nil
	expected.Operation = "control_status"
	if _, err := modulewire.EncodeInvocationStatus("control-request", "control-invocation", wrong, expected); err == nil {
		t.Fatal("unsupported target operation accepted")
	}
}
func TestStatusChecksGeneratorOriginalTraceAndClosedResultUnion(t *testing.T) {
	_, answer := generationFixture()
	answer.RequestID = "original-request"
	expected := controlExpected("generator_generate")
	expected.TargetProfileDigest = answer.ProfileDigest
	expected.TargetProfileVersion = answer.ProfileVersion
	status := modulewire.InvocationStatus{TargetInvocationID: expected.TargetInvocationID, State: "succeeded", Result: &modulewire.InvocationResult{Operation: expected.Operation, Answer: &answer}}
	data, err := modulewire.EncodeInvocationStatus("control-request", "control-invocation", status, expected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := modulewire.DecodeInvocationStatus(data, "control-request", "control-invocation", expected); err != nil {
		t.Fatal(err)
	}
	expected.TargetProfileVersion++
	if _, err := modulewire.DecodeInvocationStatus(data, "control-request", "control-invocation", expected); err == nil {
		t.Fatal("cached answer used unrelated trace")
	}
	expected.TargetProfileVersion--
	completed := true
	status.Result.Completed = &completed
	if _, err := modulewire.EncodeInvocationStatus("control-request", "control-invocation", status, expected); err == nil {
		t.Fatal("ambiguous result union accepted")
	}
}
func TestStatusRejectsUnknownAndNullFieldsAndCorrelationMismatch(t *testing.T) {
	expected := controlExpected("parser_parse")
	data, err := modulewire.EncodeInvocationStatus("control-request", "control-invocation", modulewire.InvocationStatus{TargetInvocationID: expected.TargetInvocationID, State: "running"}, expected)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(map[string]any){func(x map[string]any) { x["invocation_id"] = "foreign" }, func(x map[string]any) { x["result"].(map[string]any)["result"] = nil }, func(x map[string]any) { x["result"].(map[string]any)["cause"] = "SECRET" }} {
		var value map[string]any
		if json.Unmarshal(data, &value) != nil {
			t.Fatal("bad test fixture")
		}
		mutate(value)
		bad, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := modulewire.DecodeInvocationStatus(bad, "control-request", "control-invocation", expected); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("invalid payload accepted or unsafe cause")
		}
	}
}

func TestStatusCannotSilentlyReplaceInvalidUTF8Identifiers(t *testing.T) {
	expected := controlExpected("parser_parse")
	status := modulewire.InvocationStatus{TargetInvocationID: expected.TargetInvocationID, State: "uncertain"}
	bad := contracts.ID(string([]byte{0xff}))
	if _, err := modulewire.EncodeInvocationStatus(bad, "control-invocation", status, expected); err == nil {
		t.Fatal("request identifier was normalized by JSON serializer")
	}
	expected.TargetInvocationID = bad
	status.TargetInvocationID = bad
	if _, err := modulewire.EncodeInvocationStatus("control-request", "control-invocation", status, expected); err == nil {
		t.Fatal("target identifier was normalized by JSON serializer")
	}
}
