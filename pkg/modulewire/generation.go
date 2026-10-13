package modulewire

import (
	"encoding/json"
	"math"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type generateInput struct {
	Question string
	Evidence []contracts.Evidence
	Budget   contracts.ExecutionBudget
}
type metaFrame struct {
	Type      string
	RequestID contracts.ID
}
type deltaFrame struct{ Type, Text string }
type citationFrame struct {
	Type     string
	Citation contracts.Citation
}
type doneFrame struct {
	Type   string
	Result contracts.AnswerResult
}
type errorFrame struct {
	Type  string
	Error contracts.PublicError
}

func init() {
	register(invocation[generateInput]{}, "Context:context Input:input")
	register(generateInput{}, "Question:question Evidence:evidence Budget:budget")
	register(contracts.ExecutionBudget{}, "Deadline:deadline MaxOutputTokens:max_output_tokens MaxExtraRounds:max_extra_rounds")
	register(contracts.AnswerResult{}, "RequestID:request_id Answer:answer Citations:citations Status:status ProfileVersion:profile_version ProfileDigest:profile_digest TimingsMS:timings Degraded:degraded")
	register(contracts.Degradation{}, "Capability:capability Reason:reason")
	register(metaFrame{}, "Type:type RequestID:request_id")
	register(deltaFrame{}, "Type:type Text:text")
	register(citationFrame{}, "Type:type Citation:citation")
	register(doneFrame{}, "Type:type Result:result")
	register(errorFrame{}, "Type:type Error:error")
}

func checkGenerate(request contracts.GenerateRequest) error {
	if request.Question == "" || len(request.Evidence) < 1 || len(request.Evidence) > 1000 || request.Budget.Deadline.IsZero() ||
		request.Budget.Deadline.After(request.Request.Deadline) || request.Budget.MaxOutputTokens < 1 || request.Budget.MaxOutputTokens > 32768 ||
		request.Budget.MaxExtraRounds < 0 || request.Budget.MaxExtraRounds > 2 {
		return invalid()
	}
	for _, evidence := range request.Evidence {
		if evidence.Rank < 1 || !validCitationID(evidence.Citation) {
			return invalid()
		}
	}
	return nil
}
func validCitationID(citation contracts.Citation) bool {
	return citation.DocumentID != "" && citation.RevisionID != "" && citation.BlockID != ""
}
func EncodeGenerateRequest(call Call, request contracts.GenerateRequest) ([]byte, error) {
	if err := checkGenerate(request); err != nil {
		return nil, err
	}
	return encodeInvocation(call, request.Request, request.Scope, generateInput{request.Question, request.Evidence, request.Budget})
}
func DecodeGenerateRequest(data []byte) (Call, contracts.GenerateRequest, error) {
	context, input, err := decodeInvocation[generateInput](data)
	if err != nil {
		return Call{}, contracts.GenerateRequest{}, err
	}
	request := contracts.GenerateRequest{Request: requestContext(context), Scope: context.Scope, Question: input.Question, Evidence: input.Evidence, Budget: input.Budget}
	if err := checkGenerate(request); err != nil {
		return Call{}, contracts.GenerateRequest{}, err
	}
	return Call{context.InvocationID, context.ModuleInstanceID}, request, nil
}

// EncodeAnswerEvent converts the in-process union to one event-data DTO.
// requestID supplies meta identity, which has no separate native AnswerEvent field.
func EncodeAnswerEvent(event contracts.AnswerEvent, requestID contracts.ID) ([]byte, error) {
	if err := checkAnswerEvent(event, requestID); err != nil {
		return nil, err
	}
	switch event.Kind {
	case "meta":
		return encode(metaFrame{event.Kind, requestID})
	case "answer_delta":
		return encode(deltaFrame{event.Kind, event.Text})
	case "citation":
		return encode(citationFrame{event.Kind, *event.Citation})
	case "done":
		return encode(doneFrame{event.Kind, *event.Result})
	case "error":
		failure, err := NormalizeRemoteError(*event.Error, requestID)
		if err != nil {
			return nil, err
		}
		return encode(errorFrame{event.Kind, failure})
	}
	return nil, invalid()
}
func DecodeAnswerEvent(data []byte, requestID contracts.ID) (contracts.AnswerEvent, error) {
	if len(data) > 4<<20 {
		return contracts.AnswerEvent{}, invalid()
	}
	var tag struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &tag) != nil {
		return contracts.AnswerEvent{}, invalid()
	}
	var event contracts.AnswerEvent
	event.Kind = tag.Type
	switch tag.Type {
	case "meta":
		var frame metaFrame
		if err := decode(data, &frame); err != nil {
			return event, err
		}
		if frame.RequestID != requestID {
			return event, invalid()
		}
	case "answer_delta":
		var frame deltaFrame
		if err := decode(data, &frame); err != nil {
			return event, err
		}
		event.Text = frame.Text
	case "citation":
		var frame citationFrame
		if err := decode(data, &frame); err != nil {
			return event, err
		}
		event.Citation = &frame.Citation
	case "done":
		var frame doneFrame
		if err := decode(data, &frame); err != nil {
			return event, err
		}
		event.Result = &frame.Result
	case "error":
		var frame errorFrame
		if err := decode(data, &frame); err != nil {
			return event, err
		}
		failure, err := NormalizeRemoteError(frame.Error, requestID)
		if err != nil {
			return event, err
		}
		event.Error = &failure
	default:
		return event, invalid()
	}
	if err := checkAnswerEvent(event, requestID); err != nil {
		return contracts.AnswerEvent{}, err
	}
	return event, nil
}
func checkAnswerEvent(event contracts.AnswerEvent, requestID contracts.ID) error {
	if requestID == "" {
		return invalid()
	}
	payloads := 0
	for _, present := range []bool{event.Citation != nil, event.Result != nil, event.Error != nil} {
		if present {
			payloads++
		}
	}
	switch event.Kind {
	case "meta":
		if payloads != 0 || event.Text != "" {
			return invalid()
		}
	case "answer_delta":
		if payloads != 0 {
			return invalid()
		}
	case "citation":
		if payloads != 1 || event.Citation == nil || event.Text != "" || !validCitationID(*event.Citation) {
			return invalid()
		}
	case "done":
		if payloads != 1 || event.Result == nil || event.Text != "" {
			return invalid()
		}
		result := event.Result
		if result.RequestID != requestID || result.ProfileVersion == 0 || !digestValue(result.ProfileDigest) ||
			(result.Status != "answered" && result.Status != "insufficient_evidence") {
			return invalid()
		}
		for _, citation := range result.Citations {
			if !validCitationID(citation) {
				return invalid()
			}
		}
		for _, timing := range result.TimingsMS {
			if timing < 0 || math.IsNaN(timing) || math.IsInf(timing, 0) {
				return invalid()
			}
		}
		for _, degraded := range result.Degraded {
			if degraded.Capability == "" || !contains([]string{"unavailable", "deadline", "policy_disabled"}, degraded.Reason) {
				return invalid()
			}
		}
	case "error":
		if payloads != 1 || event.Error == nil || event.Text != "" {
			return invalid()
		}
		if _, err := NormalizeRemoteError(*event.Error, requestID); err != nil {
			return err
		}
	default:
		return invalid()
	}
	return nil
}

// Keep an exact coordinate match; a model cannot widen or invent approved evidence.
func citationIn(citation contracts.Citation, allowed []contracts.Citation) bool {
	for _, candidate := range allowed {
		if reflect.DeepEqual(citation, candidate) {
			return true
		}
	}
	return false
}
