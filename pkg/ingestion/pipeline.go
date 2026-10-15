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

// ProcessCandidate retains the observed draft version for later transactional
// CAS. It never writes a draft or approves/publishes parsed content.
func (p Pipeline) ProcessCandidate(ctx context.Context, req contracts.ParseRequest, expectedVersion uint64) (document.ParseCandidate, error) {
	if ctx == nil || p.Parser == nil {
		return document.ParseCandidate{}, errors.New("parser unavailable")
	}
	if err := ctx.Err(); err != nil {
		return document.ParseCandidate{}, err
	}
	parsed, err := p.Parser.Parse(ctx, req)
	if err != nil {
		return document.ParseCandidate{}, err
	}
	if err := ctx.Err(); err != nil {
		return document.ParseCandidate{}, err
	}
	return document.CandidateFromParse(req, expectedVersion, parsed)
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
