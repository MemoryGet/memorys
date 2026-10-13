package modulewire

import (
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type GenerationExpectation struct {
	RequestID      contracts.ID
	ProfileVersion uint64
	ProfileDigest  contracts.Digest
	Citations      []contracts.Citation
}
type AnswerLimits struct {
	MaxEvents    int
	MaxTextBytes int64
}

// AnswerSequence validates one sequential event stream. It is not concurrency safe,
// and does not validate current access, HTTP/SSE framing, token usage or deadlines.
type AnswerSequence struct {
	expected                  GenerationExpectation
	limits                    AnswerLimits
	events                    int
	started, terminal, failed bool
	text                      strings.Builder
	hasDelta                  bool
	emitted                   []contracts.Citation
	terminalError             *contracts.PublicError
}

func NewAnswerSequence(expected GenerationExpectation, limits AnswerLimits) (*AnswerSequence, error) {
	if expected.RequestID == "" || !utf8.ValidString(string(expected.RequestID)) || expected.ProfileVersion == 0 || !digestValue(expected.ProfileDigest) || len(expected.Citations) > 1000 || limits.MaxEvents < 2 || limits.MaxTextBytes < 0 {
		return nil, invalid()
	}
	// Deep-copy nested anchors so caller mutation cannot broaden the whitelist.
	copyExpected := expected
	copyExpected.Citations = nil
	for _, citation := range expected.Citations {
		if !validCitationID(citation) {
			return nil, invalid()
		}
		data, err := encode(citation)
		if err != nil {
			return nil, err
		}
		var clone contracts.Citation
		if err := decode(data, &clone); err != nil {
			return nil, err
		}
		copyExpected.Citations = append(copyExpected.Citations, clone)
	}
	return &AnswerSequence{expected: copyExpected, limits: limits}, nil
}
func (sequence *AnswerSequence) Accept(event contracts.AnswerEvent) error {
	if sequence.failed {
		return invalid()
	}
	if err := sequence.accept(event); err != nil {
		sequence.failed = true
		return err
	}
	return nil
}
func (sequence *AnswerSequence) accept(event contracts.AnswerEvent) error {
	if sequence.terminal || sequence.events >= sequence.limits.MaxEvents {
		return invalid()
	}
	if err := checkAnswerEvent(event, sequence.expected.RequestID); err != nil {
		return err
	}
	if !utf8.ValidString(event.Text) {
		return invalid()
	}
	sequence.events++
	if !sequence.started {
		if event.Kind != "meta" {
			return invalid()
		}
		sequence.started = true
		return nil
	}
	if event.Kind == "meta" {
		return invalid()
	}
	switch event.Kind {
	case "answer_delta":
		if int64(len(event.Text)) > sequence.limits.MaxTextBytes-int64(sequence.text.Len()) {
			return invalid()
		}
		sequence.text.WriteString(event.Text)
		sequence.hasDelta = true
	case "citation":
		if !citationIn(*event.Citation, sequence.expected.Citations) {
			return invalid()
		}
		// Own a copy; do not let a caller alter a citation after validation.
		data, err := encode(*event.Citation)
		if err != nil {
			return err
		}
		var clone contracts.Citation
		if err := decode(data, &clone); err != nil {
			return err
		}
		sequence.emitted = append(sequence.emitted, clone)
	case "done":
		result := event.Result
		if result.ProfileDigest != sequence.expected.ProfileDigest || result.ProfileVersion != sequence.expected.ProfileVersion ||
			!utf8.ValidString(result.Answer) || int64(len(result.Answer)) > sequence.limits.MaxTextBytes ||
			(sequence.hasDelta && result.Answer != sequence.text.String()) {
			return invalid()
		}
		if result.Status == "answered" && (result.Answer == "" || len(result.Citations) == 0) {
			return invalid()
		}
		for _, citation := range result.Citations {
			if !citationIn(citation, sequence.expected.Citations) {
				return invalid()
			}
		}
		for _, citation := range sequence.emitted {
			if !citationIn(citation, result.Citations) {
				return invalid()
			}
		}
		sequence.terminal = true
	case "error":
		failure, err := NormalizeRemoteError(*event.Error, sequence.expected.RequestID)
		if err != nil {
			return err
		}
		sequence.terminalError = &failure
		sequence.terminal = true
	default:
		return invalid()
	}
	return nil
}

// Finish rejects a disconnect without a terminal and never calls an error answer successful.
func (sequence *AnswerSequence) Finish() error {
	if sequence.failed || !sequence.started || !sequence.terminal {
		sequence.failed = true
		return invalid()
	}
	if sequence.terminalError != nil {
		return *sequence.terminalError
	}
	return nil
}
