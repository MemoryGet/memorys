package queryread

import (
	"context"
	"errors"
	"sync"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrClosed = errors.New("query read handle is closed")

// Guard serializes access to a callback-scoped repository. Its host must defer
// Close before returning from WithinRead. It does not create a DB snapshot.
type Guard struct {
	mu       sync.Mutex
	closed   bool
	ctx      context.Context
	cancel   context.CancelFunc
	backend  contracts.QueryReadRepository
	scope    contracts.AccessScope
	identity contracts.QueryReadIdentity
}

var _ contracts.QueryReadRepository = (*Guard)(nil)

func NewGuard(ctx context.Context, backend contracts.QueryReadRepository, scope contracts.AccessScope, identity contracts.QueryReadIdentity) (*Guard, error) {
	if ctx == nil || backend == nil || scope.ExpiresAt.IsZero() {
		return nil, ErrInvalid
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, ErrInvalid
	}
	if scope.ExpiresAt.Before(deadline) {
		deadline = scope.ExpiresAt
	}
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	request := contracts.PublishedPageRequest{Identity: identity, KnowledgeBaseIDs: scope.KnowledgeBaseIDs, Limit: 1}
	if contracts.ValidatePublishedPage(contracts.PublishedPage{Identity: identity}, request, scope, "") != nil {
		return nil, ErrInvalid
	}
	child, cancel := context.WithDeadline(ctx, deadline)
	if err := child.Err(); err != nil {
		cancel()
		return nil, err
	}
	return &Guard{ctx: child, cancel: cancel, backend: backend, scope: scope, identity: identity}, nil
}

// Close cancels before waiting for serialized calls. Cooperative adapters must
// exit on context cancellation; ignoring it can prevent Close from returning.
func (g *Guard) Close() { g.cancel(); g.mu.Lock(); defer g.mu.Unlock(); g.closed = true }

func guarded[T any](g *Guard, ctx context.Context, call func(context.Context) (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		return zero, ErrInvalid
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return zero, ErrClosed
	}
	if err := g.ctx.Err(); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	deadline, _ := g.ctx.Deadline()
	child, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	stop := context.AfterFunc(g.ctx, cancel)
	defer stop()
	value, err := call(child)
	if err := g.ctx.Err(); err != nil {
		return zero, err
	}
	if child.Err() != nil {
		return zero, child.Err()
	}
	if err != nil {
		return zero, err
	}
	return value, nil
}

func (g *Guard) ReadPublishedPage(ctx context.Context, r contracts.PublishedPageRequest) (contracts.PublishedPage, error) {
	return guarded(g, ctx, func(c context.Context) (contracts.PublishedPage, error) {
		if r.Identity != g.identity || contracts.ValidatePublishedPage(contracts.PublishedPage{Identity: g.identity}, r, g.scope, "") != nil {
			return contracts.PublishedPage{}, ErrInvalid
		}
		expected := r
		expected.KnowledgeBaseIDs = append([]contracts.ID(nil), r.KnowledgeBaseIDs...)
		r.KnowledgeBaseIDs = append([]contracts.ID(nil), r.KnowledgeBaseIDs...)
		page, err := g.backend.ReadPublishedPage(c, r)
		if err != nil {
			return contracts.PublishedPage{}, ErrRead
		}
		if contracts.ValidatePublishedPage(page, expected, g.scope, "") != nil {
			return contracts.PublishedPage{}, ErrInvalid
		}
		return page, nil
	})
}

func (g *Guard) allowed(kb contracts.ID) bool {
	for _, id := range g.scope.KnowledgeBaseIDs {
		if kb == id {
			return true
		}
	}
	return false
}

func (g *Guard) LoadActiveEpoch(ctx context.Context, kb contracts.ID) (contracts.BackendEpoch, error) {
	return guarded(g, ctx, func(c context.Context) (contracts.BackendEpoch, error) {
		if !g.allowed(kb) {
			return contracts.BackendEpoch{}, ErrInvalid
		}
		epoch, err := g.backend.LoadActiveEpoch(c, kb)
		if err != nil {
			return contracts.BackendEpoch{}, ErrRead
		}
		if epoch.ID == "" || epoch.KnowledgeBaseID != kb || epoch.State != "active" {
			return contracts.BackendEpoch{}, ErrInvalid
		}
		return epoch, nil
	})
}

func (g *Guard) LoadCurrentDocument(ctx context.Context, id contracts.ID) (contracts.CurrentDocumentState, error) {
	return guarded(g, ctx, func(c context.Context) (contracts.CurrentDocumentState, error) {
		if id == "" {
			return contracts.CurrentDocumentState{}, ErrInvalid
		}
		state, err := g.backend.LoadCurrentDocument(c, id)
		if err != nil {
			return contracts.CurrentDocumentState{}, ErrRead
		}
		if state.Document.ID != id || state.Document.SpaceID != g.scope.SpaceID || !g.allowed(state.Document.KnowledgeBaseID) || state.Document.RowVersion == 0 {
			return contracts.CurrentDocumentState{}, ErrInvalid
		}
		return state, nil
	})
}
