package ingestion

import (
	"context"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type Pipeline struct{ Parser contracts.Parser }
type Result struct {
	DraftID contracts.ID
	Blocks  []contracts.DraftBlock
	Blobs   []contracts.BlobRef
}

func (p Pipeline) Process(ctx context.Context, req contracts.ParseRequest) (Result, error) {
	if ctx == nil || p.Parser == nil {
		return Result{}, errors.New("parser unavailable")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	parsed, err := p.Parser.Parse(ctx, req)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	blocks, err := document.FromParsedBlocks(req.DraftID, parsed.Blocks)
	if err != nil {
		return Result{}, err
	}
	return Result{DraftID: req.DraftID, Blocks: blocks, Blobs: []contracts.BlobRef{req.Input}}, nil
}
