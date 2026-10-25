package queryread

import (
	"context"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// LoadCurrentDocuments keeps optional batching behind the existing read handle.
// The guard enforces scope, exact membership/order, cancellation and lifetime.
func LoadCurrentDocuments(ctx context.Context, repo contracts.QueryReadRepository, ids []contracts.ID) ([]contracts.CurrentDocumentState, error) {
	if ctx == nil || repo == nil || len(ids) < 1 || len(ids) > 1000 {
		return nil, ErrInvalid
	}
	seen := map[contracts.ID]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
	}
	if batch, ok := repo.(contracts.QueryReadBatchRepository); ok {
		return batch.LoadCurrentDocuments(ctx, append([]contracts.ID(nil), ids...))
	}
	out := make([]contracts.CurrentDocumentState, 0, len(ids))
	for _, id := range ids {
		s, e := repo.LoadCurrentDocument(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, nil
}

func (g *Guard) LoadCurrentDocuments(ctx context.Context, ids []contracts.ID) ([]contracts.CurrentDocumentState, error) {
	expected := append([]contracts.ID(nil), ids...)
	return guarded(g, ctx, func(c context.Context) ([]contracts.CurrentDocumentState, error) {
		rows, e := LoadCurrentDocuments(c, g.backend, expected)
		if e != nil || len(rows) != len(expected) {
			return nil, ErrRead
		}
		for n, state := range rows {
			d := state.Document
			if d.ID != expected[n] || d.SpaceID != g.scope.SpaceID || !g.allowed(d.KnowledgeBaseID) || d.RowVersion == 0 {
				return nil, ErrInvalid
			}
		}
		return rows, nil
	})
}
