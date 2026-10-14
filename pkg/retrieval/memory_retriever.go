package retrieval

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/index"
)

type MemoryRetriever struct{ Index *index.MemoryIndex }

func (r MemoryRetriever) Search(ctx context.Context, req contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	if r.Index == nil {
		return nil, index.ErrIndex
	}
	return r.Index.Search(ctx, req)
}
