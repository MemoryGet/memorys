package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type PartnerAskInput struct {
	Credential       contracts.Credential
	Request          contracts.RequestContext
	SpaceID          contracts.ID
	KnowledgeBaseIDs []contracts.ID
	Question         string
	Reflection       bool
}

type PartnerAskApplication interface {
	Ask(context.Context, PartnerAskInput, contracts.AnswerSink) error
}

type PartnerAskHandler struct{ Service PartnerAskApplication }

func (h PartnerAskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := requestNonce(w)
	fail := func(status int, code, message string) {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "safe_message": message, "retryable": status == 503 || status == 504, "request_id": requestID})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(405, "invalid_argument", "POST required")
		return
	}
	if h.Service == nil {
		fail(503, "capability_missing", "Answer not configured")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(415, "invalid_argument", "JSON required")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		fail(400, "invalid_argument", "Invalid ask request")
		return
	}
	if _, err = canonical.JSON(raw); err != nil {
		fail(400, "invalid_argument", "Invalid ask request")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		fail(400, "invalid_argument", "Invalid ask request")
		return
	}
	for key := range fields {
		switch key {
		case "question", "space_id", "kb_ids", "reflection", "stream":
		default:
			fail(400, "invalid_argument", "Invalid ask request")
			return
		}
	}
	var input struct {
		Question   string         `json:"question"`
		SpaceID    contracts.ID   `json:"space_id"`
		KBIDs      []contracts.ID `json:"kb_ids"`
		Reflection bool           `json:"reflection"`
		Stream     bool           `json:"stream"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || !utf8.ValidString(input.Question) || utf8.RuneCountInString(input.Question) < 1 || utf8.RuneCountInString(input.Question) > 10000 || !partnerID(input.SpaceID) {
		fail(400, "invalid_argument", "Invalid ask request")
		return
	}
	if _, present := fields["kb_ids"]; present {
		if input.KBIDs == nil || len(input.KBIDs) > 1000 {
			fail(400, "invalid_argument", "Invalid knowledge bases")
			return
		}
		seen := map[contracts.ID]bool{}
		for _, id := range input.KBIDs {
			if !partnerID(id) || seen[id] {
				fail(400, "invalid_argument", "Invalid knowledge bases")
				return
			}
			seen[id] = true
		}
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || len(headers[0]) > 16400 {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	scheme, token, ok := strings.Cut(headers[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n,") {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte(token)}
	defer clear(credential.Secret)
	request := PartnerAskInput{Credential: credential, Request: contracts.RequestContext{RequestID: requestID, Deadline: deadline}, SpaceID: input.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), input.KBIDs...), Question: input.Question, Reflection: input.Reflection}
	var result *contracts.AnswerResult
	if input.Stream {
		flusher, ok := w.(http.Flusher)
		if !ok {
			fail(500, "internal", "Streaming unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		emittedDone := false
		send := func(event string, value any) error {
			body, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				return marshalErr
			}
			if _, writeErr := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body); writeErr != nil {
				return writeErr
			}
			flusher.Flush()
			return nil
		}
		err = h.Service.Ask(ctx, request, func(event contracts.AnswerEvent) error {
			switch event.Kind {
			case "meta":
				return send("meta", map[string]any{"type": "meta", "request_id": requestID})
			case "answer_delta":
				if !utf8.ValidString(event.Text) || event.Text == "" {
					return errors.New("invalid answer delta")
				}
				return send("answer_delta", map[string]any{"type": "answer_delta", "text": event.Text})
			case "citation":
				if event.Citation == nil {
					return errors.New("invalid citation event")
				}
				return send("citation", map[string]any{"type": "citation", "citation": map[string]any{"document_id": event.Citation.DocumentID, "revision_id": event.Citation.RevisionID, "block_id": event.Citation.BlockID, "source_anchor": sourceDTO(event.Citation.Source)}})
			case "done":
				if event.Result == nil || emittedDone || event.Result.RequestID != requestID || !validAnswerResult(*event.Result) {
					return errors.New("invalid answer result")
				}
				emittedDone = true
				result = event.Result
				return send("done", map[string]any{"type": "done", "result": answerResultDTO(*event.Result)})
			case "error":
				return errors.New("answer failed")
			default:
				return errors.New("unknown answer event")
			}
		})
		if err == nil && !emittedDone {
			err = errors.New("answer did not finish")
		}
		if err != nil && !emittedDone {
			_ = send("error", map[string]any{"type": "error", "error": map[string]any{"code": "dependency_unavailable", "safe_message": "Answer unavailable", "retryable": true, "request_id": requestID}})
		}
		return
	}
	err = h.Service.Ask(ctx, request, func(event contracts.AnswerEvent) error {
		if event.Kind == "done" && event.Result != nil {
			result = event.Result
		}
		return nil
	})
	if ctx.Err() != nil {
		fail(504, "deadline_exceeded", "Answer deadline exceeded")
		return
	}
	if errors.Is(err, ErrPartnerAskUnauthenticated) {
		fail(401, "unauthenticated", "Authentication required")
		return
	}
	if err != nil || result == nil || result.RequestID != requestID || !validAnswerResult(*result) {
		fail(503, "dependency_unavailable", "Answer unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(answerResultDTO(*result))
}

var ErrPartnerAskUnauthenticated = errors.New("answer authentication required")

func answerResultDTO(result contracts.AnswerResult) map[string]any {
	citations := make([]map[string]any, 0, len(result.Citations))
	for _, citation := range result.Citations {
		citations = append(citations, map[string]any{"document_id": citation.DocumentID, "revision_id": citation.RevisionID, "block_id": citation.BlockID, "source_anchor": sourceDTO(citation.Source)})
	}
	degraded := make([]map[string]any, 0, len(result.Degraded))
	for _, item := range result.Degraded {
		degraded = append(degraded, map[string]any{"capability": item.Capability, "reason": item.Reason})
	}
	timings := result.TimingsMS
	if timings == nil {
		timings = map[string]float64{}
	}
	return map[string]any{"request_id": result.RequestID, "profile_version": result.ProfileVersion, "profile_digest": result.ProfileDigest, "timings": timings, "degraded": degraded, "answer": result.Answer, "citations": citations, "status": result.Status}
}

func validAnswerResult(result contracts.AnswerResult) bool {
	if !partnerID(result.RequestID) || result.ProfileVersion == 0 || len(result.ProfileDigest) != 64 || result.Answer == "" || !utf8.ValidString(result.Answer) || result.Status != "answered" && result.Status != "insufficient_evidence" {
		return false
	}
	decoded, err := hex.DecodeString(string(result.ProfileDigest))
	if err != nil || len(decoded) != 32 || strings.ToLower(string(result.ProfileDigest)) != string(result.ProfileDigest) {
		return false
	}
	if result.Status == "answered" && len(result.Citations) == 0 {
		return false
	}
	for _, citation := range result.Citations {
		if !validPartnerCitation(citation) {
			return false
		}
	}
	return true
}
