package parser

import (
	"context"
	"errors"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type dispatchParserFake struct{ format string }

func (f dispatchParserFake) Parse(_ context.Context, req contracts.ParseRequest) (contracts.ParseResult, error) {
	if req.Format != f.format {
		return contracts.ParseResult{}, errors.New("wrong parser")
	}
	return contracts.ParseResult{ParserVersion: f.format + "/1"}, nil
}

func TestDispatchParserSelectsCanonicalFormat(t *testing.T) {
	p := DispatchParser{Parsers: map[string]contracts.Parser{"docx": dispatchParserFake{format: "docx"}}}
	result, err := p.Parse(context.Background(), contracts.ParseRequest{Format: "docx"})
	if err != nil || result.ParserVersion != "docx/1" {
		t.Fatalf("dispatch result=%+v err=%v", result, err)
	}
	if _, err := p.Parse(context.Background(), contracts.ParseRequest{Format: "pptx"}); !errors.Is(err, ErrDispatch) {
		t.Fatal("unknown format reached parser")
	}
}
