package moduletransport_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/moduletransport"
)

type lateFailureBody struct{ io.Reader }

func (body lateFailureBody) Read(p []byte) (int, error) {
	n, err := body.Reader.Read(p)
	if err == io.EOF {
		return n, errors.New("SECRET late connection error")
	}
	return n, err
}
func (lateFailureBody) Close() error { return nil }

func TestTerminalWaitsForEOFAndFreshAccessGate(t *testing.T) {
	text, expected := streamFixture(t, "\n")
	response, _ := memoryResponse(text)
	response.Body = lateFailureBody{strings.NewReader(text)}
	terminal := 0
	sink := func(event contracts.AnswerEvent) error {
		if event.Kind == "done" {
			terminal++
		}
		return nil
	}
	err := moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, limits(), openGate, sink)
	if !errors.Is(err, moduletransport.ErrProtocol) || terminal != 0 || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("late network failure published success", err, terminal)
	}
	response, _ = memoryResponse(text)
	checks := 0
	err = moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, limits(), func(context.Context) error {
		checks++
		if checks == 3 {
			return errors.New("revoked before terminal")
		}
		return nil
	}, sink)
	if !errors.Is(err, moduletransport.ErrAccess) || terminal != 0 {
		t.Fatal("terminal bypassed fresh access gate", err, terminal)
	}
}
