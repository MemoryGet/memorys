package parser

import (
	"context"
	"errors"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrDispatch = errors.New("parser dispatch rejected")

// DispatchParser binds each canonical upload format to a host-supplied
// parser. It keeps format selection outside the durable task payload while
// ensuring an unknown format fails before any source bytes are opened.
type DispatchParser struct {
	Parsers map[string]contracts.Parser
}

func (p DispatchParser) Parse(ctx context.Context, req contracts.ParseRequest) (contracts.ParseResult, error) {
	if ctx == nil || req.Format == "" || p.Parsers == nil {
		return contracts.ParseResult{}, ErrDispatch
	}
	selected := p.Parsers[req.Format]
	if selected == nil {
		return contracts.ParseResult{}, ErrDispatch
	}
	return selected.Parse(ctx, req)
}
