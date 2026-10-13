package modulewire_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func generationFixture() (contracts.GenerateRequest, contracts.AnswerResult) {
	base := parseRequest()
	citation := contracts.Citation{DocumentID: "doc-1", RevisionID: "rev-1", BlockID: "block-1", Source: contracts.SourceAnchor{Kind: "original", BlobID: "blob-1", Locator: contracts.Locator{Precision: "coarse"}}}
	request := contracts.GenerateRequest{Request: base.Request, Scope: base.Scope, Question: "设备怎么修？", Evidence: []contracts.Evidence{{Citation: citation, SourceExcerpt: "检查电源😀", Rank: 1}}, Budget: contracts.ExecutionBudget{Deadline: base.Request.Deadline, MaxOutputTokens: 100, MaxExtraRounds: 2}}
	result := contracts.AnswerResult{RequestID: base.Request.RequestID, Answer: "检查电源😀", Citations: []contracts.Citation{citation}, Status: "answered", ProfileVersion: 9007199254740993, ProfileDigest: base.Request.ProfileDigest, TimingsMS: map[string]float64{"generate": 1.25}, Degraded: []contracts.Degradation{}}
	return request, result
}
func generationSequence(t *testing.T, request contracts.GenerateRequest, result contracts.AnswerResult) *modulewire.AnswerSequence {
	t.Helper()
	citations := []contracts.Citation{request.Evidence[0].Citation}
	sequence, err := modulewire.NewAnswerSequence(modulewire.GenerationExpectation{RequestID: request.Request.RequestID, ProfileVersion: result.ProfileVersion, ProfileDigest: request.Request.ProfileDigest, Citations: citations}, modulewire.AnswerLimits{MaxEvents: 10, MaxTextBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	return sequence
}
func TestGenerateRequestAndFiveEventVariantsRoundTrip(t *testing.T) {
	request, result := generationFixture()
	call := modulewire.Call{InvocationID: "i", ModuleInstanceID: "generator-a"}
	data, err := modulewire.EncodeGenerateRequest(call, request)
	if err != nil {
		t.Fatal(err)
	}
	gotCall, got, err := modulewire.DecodeGenerateRequest(data)
	if err != nil || gotCall != call || !reflect.DeepEqual(request, got) {
		t.Fatal("generate request lost fields", err)
	}
	failure := contracts.PublicError{Code: "internal", SafeMessage: "Internal failure", RequestID: request.Request.RequestID}
	for _, event := range []contracts.AnswerEvent{{Kind: "meta"}, {Kind: "answer_delta", Text: result.Answer}, {Kind: "citation", Citation: &result.Citations[0]}, {Kind: "done", Result: &result}, {Kind: "error", Error: &failure}} {
		data, err := modulewire.EncodeAnswerEvent(event, request.Request.RequestID)
		if err != nil {
			t.Fatal(event.Kind, err)
		}
		actual, err := modulewire.DecodeAnswerEvent(data, request.Request.RequestID)
		if err != nil || !reflect.DeepEqual(event, actual) {
			t.Fatal("event lost fields", event.Kind, err)
		}
	}
	request.Budget.Deadline = request.Request.Deadline.Add(time.Nanosecond)
	if _, err := modulewire.EncodeGenerateRequest(call, request); err == nil {
		t.Fatal("budget extended past request deadline")
	}
}
func TestAnswerSequenceRequiresMetaUniqueTerminalAndCompleteText(t *testing.T) {
	request, result := generationFixture()
	sequence := generationSequence(t, request, result)
	for _, event := range []contracts.AnswerEvent{{Kind: "meta"}, {Kind: "answer_delta", Text: result.Answer}, {Kind: "citation", Citation: &result.Citations[0]}, {Kind: "done", Result: &result}} {
		if err := sequence.Accept(event); err != nil {
			t.Fatal(err)
		}
	}
	if sequence.Finish() != nil {
		t.Fatal("valid complete stream rejected")
	}
	if sequence.Accept(contracts.AnswerEvent{Kind: "done", Result: &result}) == nil || sequence.Finish() == nil {
		t.Fatal("extra terminal did not invalidate stream")
	}
	sequence = generationSequence(t, request, result)
	if sequence.Accept(contracts.AnswerEvent{Kind: "answer_delta", Text: "early"}) == nil {
		t.Fatal("missing meta accepted")
	}
	sequence = generationSequence(t, request, result)
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "meta"})
	if sequence.Finish() == nil {
		t.Fatal("disconnect became success")
	}
}
func TestAnswerSequenceRejectsForeignCitationTraceAndContradictingResult(t *testing.T) {
	request, result := generationFixture()
	for _, change := range []func(*contracts.AnswerResult){func(r *contracts.AnswerResult) {
		r.Citations = []contracts.Citation{{DocumentID: "foreign", RevisionID: "rev-1", BlockID: "block-1"}}
	}, func(r *contracts.AnswerResult) { r.ProfileVersion++ }, func(r *contracts.AnswerResult) { r.ProfileDigest = contracts.Digest(strings.Repeat("f", 64)) }, func(r *contracts.AnswerResult) { r.Answer = "different" }} {
		sequence := generationSequence(t, request, result)
		_ = sequence.Accept(contracts.AnswerEvent{Kind: "meta"})
		_ = sequence.Accept(contracts.AnswerEvent{Kind: "answer_delta", Text: result.Answer})
		bad := result
		change(&bad)
		if sequence.Accept(contracts.AnswerEvent{Kind: "done", Result: &bad}) == nil || sequence.Finish() == nil {
			t.Fatal("invalid result accepted")
		}
	}
	sequence := generationSequence(t, request, result)
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "meta"})
	citation := result.Citations[0]
	citation.BlockID = "outside-block"
	if sequence.Accept(contracts.AnswerEvent{Kind: "citation", Citation: &citation}) == nil {
		t.Fatal("out-of-whitelist citation accepted")
	}
}
func TestAnswerErrorTerminalIsNotSuccessAndUnionCannotCarryExtraPayload(t *testing.T) {
	request, result := generationFixture()
	sequence := generationSequence(t, request, result)
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "meta"})
	failure := contracts.PublicError{Code: "dependency_unavailable", SafeMessage: "SECRET provider text", Retryable: true, RequestID: request.Request.RequestID}
	if sequence.Accept(contracts.AnswerEvent{Kind: "error", Error: &failure}) != nil {
		t.Fatal("valid error terminal rejected")
	}
	var safe contracts.PublicError
	if !errors.As(sequence.Finish(), &safe) || safe.SafeMessage != "Dependency unavailable" {
		t.Fatal("error became success or leaked message")
	}
	if _, err := modulewire.EncodeAnswerEvent(contracts.AnswerEvent{Kind: "answer_delta", Text: "text", Result: &result}, request.Request.RequestID); err == nil {
		t.Fatal("ambiguous native union accepted")
	}
	if _, err := modulewire.DecodeAnswerEvent([]byte(`{"type":"meta","request_id":"wrong"}`), request.Request.RequestID); err == nil {
		t.Fatal("foreign meta accepted")
	}
}
func TestSequenceOwnsCitationWhitelistAndEnforcesOutputLimit(t *testing.T) {
	request, result := generationFixture()
	page := 1
	citation := result.Citations[0]
	citation.Source.Locator.Page = &page
	sequence, err := modulewire.NewAnswerSequence(modulewire.GenerationExpectation{RequestID: request.Request.RequestID, ProfileVersion: result.ProfileVersion, ProfileDigest: request.Request.ProfileDigest, Citations: []contracts.Citation{citation}}, modulewire.AnswerLimits{MaxEvents: 3, MaxTextBytes: 2})
	if err != nil {
		t.Fatal(err)
	}
	page = 2
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "meta"})
	if sequence.Accept(contracts.AnswerEvent{Kind: "citation", Citation: &citation}) == nil {
		t.Fatal("caller mutation broadened whitelist")
	}
	sequence = generationSequence(t, request, result)
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "meta"})
	if sequence.Accept(contracts.AnswerEvent{Kind: "answer_delta", Text: strings.Repeat("x", 101)}) == nil {
		t.Fatal("text budget exceeded")
	}
}

func TestSequenceCountsEventsAndKeepsEmittedCitationInFinalResult(t *testing.T) {
	request, result := generationFixture()
	sequence, err := modulewire.NewAnswerSequence(modulewire.GenerationExpectation{RequestID: request.Request.RequestID, ProfileVersion: result.ProfileVersion, ProfileDigest: request.Request.ProfileDigest, Citations: result.Citations}, modulewire.AnswerLimits{MaxEvents: 2, MaxTextBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "meta"})
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "answer_delta", Text: result.Answer})
	if sequence.Accept(contracts.AnswerEvent{Kind: "done", Result: &result}) == nil {
		t.Fatal("event budget bypassed")
	}
	sequence = generationSequence(t, request, result)
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "meta"})
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "citation", Citation: &result.Citations[0]})
	result.Status = "insufficient_evidence"
	result.Citations = nil
	if sequence.Accept(contracts.AnswerEvent{Kind: "done", Result: &result}) == nil {
		t.Fatal("emitted citation vanished from final result")
	}
	sequence = generationSequence(t, request, result)
	_ = sequence.Accept(contracts.AnswerEvent{Kind: "meta"})
	result.Answer = strings.Repeat("x", 101)
	if sequence.Accept(contracts.AnswerEvent{Kind: "done", Result: &result}) == nil {
		t.Fatal("done-only text bypassed byte budget")
	}
}
