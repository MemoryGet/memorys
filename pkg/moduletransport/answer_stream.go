// Package moduletransport implements bounded transport helpers, not a service
// authenticator, current access policy, provider, or runtime assembly.
package moduletransport

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

var ErrProtocol = errors.New("invalid answer event stream")
var ErrAccess = errors.New("answer access check failed")
var ErrSink = errors.New("answer sink failed")

type AnswerStreamLimits struct {
	MaxLineBytes int
	// MaxFrameBytes counts UTF-8 line bytes plus one normalized LF per line,
	// including comments/ignored fields. MaxWireBytes counts actual raw bytes.
	MaxFrameBytes int
	MaxWireBytes  int64
	Answer        modulewire.AnswerLimits
}

// ConsumeAnswerStream owns and always closes response.Body. A finite deadline
// and a caller-provided current-access gate are mandatory. It never reconnects.
// gate/sink must be bounded and honor the caller context; arbitrary callbacks
// or custom ReadClosers whose Close cannot interrupt Read cannot be preempted.
func ConsumeAnswerStream(ctx context.Context, response *http.Response, invocationID contracts.ID, expected modulewire.GenerationExpectation, limits AnswerStreamLimits, gate func(context.Context) error, sink contracts.AnswerSink) error {
	if response == nil || response.Body == nil {
		return ErrProtocol
	}
	var once sync.Once
	closeBody := func() { once.Do(func() { _ = response.Body.Close() }) }
	defer closeBody()
	if ctx == nil || gate == nil || sink == nil || limits.MaxLineBytes < 1 || limits.MaxFrameBytes < limits.MaxLineBytes || limits.MaxFrameBytes > 4<<20 || limits.MaxWireBytes < 1 || limits.MaxWireBytes == math.MaxInt64 {
		return ErrProtocol
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrProtocol
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, closeBody)
	defer stop()
	if response.StatusCode != http.StatusOK || !checkHeaders(response.Header, expected.RequestID, invocationID) {
		return ErrProtocol
	}
	sequence, err := modulewire.NewAnswerSequence(expected, limits.Answer)
	if err != nil {
		return ErrProtocol
	}
	reader := eventLines{reader: bufio.NewReader(io.LimitReader(response.Body, limits.MaxWireBytes+1)), ctx: ctx, maxWire: limits.MaxWireBytes, maxLine: limits.MaxLineBytes}
	var data strings.Builder
	eventName := ""
	hasData := false
	frameBytes := 0
	first := true
	var terminal *contracts.AnswerEvent
	for {
		line, err := reader.next()
		if err != nil {
			if cancel := ctx.Err(); cancel != nil {
				return cancel
			}
			if err != io.EOF {
				return ErrProtocol
			}
			// Never synthesize a blank delimiter for a truncated final frame.
			if len(line) > 0 && (!utf8.ValidString(string(line)) || line[0] != ':') {
				return ErrProtocol
			}
			if hasData || eventName != "" {
				return ErrProtocol
			}
			finished := sequence.Finish()
			if terminal == nil {
				return finished
			}
			if finished != nil {
				var failure contracts.PublicError
				if !errors.As(finished, &failure) {
					return finished
				}
			}
			if err := deliver(ctx, *terminal, gate, sink); err != nil {
				return err
			}
			return finished
		}
		frameBytes += len(line) + 1
		if frameBytes > limits.MaxFrameBytes {
			return ErrProtocol
		}
		if first {
			first = false
			line = []byte(strings.TrimPrefix(string(line), "\xef\xbb\xbf"))
		}
		if !utf8.Valid(line) {
			return ErrProtocol
		}
		text := string(line)
		if text == "" {
			if hasData {
				payload := strings.TrimSuffix(data.String(), "\n")
				event, err := modulewire.DecodeAnswerEvent([]byte(payload), expected.RequestID)
				if err != nil || eventName == "" || eventName != event.Kind {
					return ErrProtocol
				}
				if err := sequence.Accept(event); err != nil {
					return ErrProtocol
				}
				// Do not publish a success terminal before EOF can rule out
				// truncation, transport failure or contradictory trailing events.
				if event.Kind == "done" || event.Kind == "error" {
					terminal = &event
				} else if err := deliver(ctx, event, gate, sink); err != nil {
					return err
				}
			}
			eventName = ""
			data.Reset()
			hasData = false
			frameBytes = 0
			continue
		}
		if strings.HasPrefix(text, ":") {
			continue
		}
		name, value, found := strings.Cut(text, ":")
		if !found {
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch name {
		case "event":
			eventName = value
		case "data":
			hasData = true
			data.WriteString(value)
			data.WriteByte('\n')
			// id/retry/unknown fields do not enable replay or reconnection.
		}
	}
}

func deliver(ctx context.Context, event contracts.AnswerEvent, gate func(context.Context) error, sink contracts.AnswerSink) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := gate(ctx); err != nil {
		if cancel := ctx.Err(); cancel != nil {
			return cancel
		}
		return ErrAccess
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sink(event); err != nil {
		if cancel := ctx.Err(); cancel != nil {
			return cancel
		}
		return ErrSink
	}
	return ctx.Err()
}

type eventLines struct {
	reader        *bufio.Reader
	ctx           context.Context
	wire, maxWire int64
	maxLine       int
	skipLF        bool
}

func (lines *eventLines) next() ([]byte, error) {
	line := make([]byte, 0, 128)
	for {
		if err := lines.ctx.Err(); err != nil {
			return nil, err
		}
		value, err := lines.reader.ReadByte()
		if err != nil {
			return line, err
		}
		lines.wire++
		if lines.wire > lines.maxWire {
			return nil, ErrProtocol
		}
		if lines.skipLF {
			lines.skipLF = false
			if value == '\n' {
				continue
			}
		}
		switch value {
		case '\r':
			lines.skipLF = true
			return line, nil
		case '\n':
			return line, nil
		default:
			line = append(line, value)
			if len(line) > lines.maxLine {
				return nil, ErrProtocol
			}
		}
	}
}
func headerValues(headers http.Header, name string) []string {
	var values []string
	for key, entries := range headers {
		if strings.EqualFold(key, name) {
			values = append(values, entries...)
		}
	}
	return values
}
func checkHeaders(headers http.Header, requestID, invocationID contracts.ID) bool {
	if requestID == "" || invocationID == "" || !utf8.ValidString(string(requestID)) || !utf8.ValidString(string(invocationID)) {
		return false
	}
	for name, value := range map[string]string{"X-AKOS-Request-Id": string(requestID), "X-AKOS-Invocation-Id": string(invocationID)} {
		values := headerValues(headers, name)
		if len(values) != 1 || values[0] != value {
			return false
		}
	}
	values := headerValues(headers, "Content-Type")
	if len(values) != 1 {
		return false
	}
	media, params, err := mime.ParseMediaType(values[0])
	if err != nil || media != "text/event-stream" {
		return false
	}
	if charset, present := params["charset"]; present && !strings.EqualFold(charset, "utf-8") {
		return false
	}
	return true
}
