package queryread_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type blockingRepository struct {
	*repository
	entered chan struct{}
	stopped atomic.Bool
}

func (r *blockingRepository) ReadPublishedPage(c context.Context, _ contracts.PublishedPageRequest) (contracts.PublishedPage, error) {
	close(r.entered)
	<-c.Done()
	r.stopped.Store(true)
	return contracts.PublishedPage{}, c.Err()
}
func guardScope() contracts.AccessScope {
	s := scope()
	s.ExpiresAt = time.Now().Add(time.Minute)
	return s
}
func guardRequest() contracts.PublishedPageRequest {
	return contracts.PublishedPageRequest{Identity: identity, KnowledgeBaseIDs: []contracts.ID{"kb"}, Limit: 1}
}

func TestGuardInvalidatesEscapedReferenceAndCloseIsIdempotent(t *testing.T) {
	r := &repository{pages: []contracts.PublishedPage{page("a", "ra", "ga", "")}}
	g, e := queryread.NewGuard(ctx(t), r, guardScope(), identity)
	if e != nil {
		t.Fatal(e)
	}
	var escaped contracts.QueryReadRepository = g
	g.Close()
	g.Close()
	if _, e := escaped.ReadPublishedPage(context.Background(), guardRequest()); e != queryread.ErrClosed || len(r.calls) != 0 {
		t.Fatal("closed handle called repository", e)
	}
	if _, e := escaped.LoadActiveEpoch(context.Background(), "kb"); e != queryread.ErrClosed {
		t.Fatal(e)
	}
	if _, e := escaped.LoadCurrentDocument(context.Background(), "doc"); e != queryread.ErrClosed {
		t.Fatal(e)
	}
}

func TestGuardCloseCancelsActiveReadAndWaits(t *testing.T) {
	r := &blockingRepository{repository: &repository{}, entered: make(chan struct{})}
	g, e := queryread.NewGuard(ctx(t), r, guardScope(), identity)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := g.ReadPublishedPage(context.Background(), guardRequest()); done <- e }()
	select {
	case <-r.entered:
	case <-time.After(time.Second):
		t.Fatal("read never started")
	}
	g.Close()
	if !r.stopped.Load() {
		t.Fatal("Close did not join active read")
	}
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestGuardIdentityAndScopeAreFrozen(t *testing.T) {
	r := &repository{pages: []contracts.PublishedPage{page("a", "ra", "ga", "")}}
	s := guardScope()
	g, e := queryread.NewGuard(ctx(t), r, s, identity)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	s.KnowledgeBaseIDs[0] = "other"
	q := guardRequest()
	q.Identity.ReadVersion = "other"
	if _, e := g.ReadPublishedPage(context.Background(), q); e != queryread.ErrInvalid || len(r.calls) != 0 {
		t.Fatal("wrong view invoked backend", e)
	}
	if _, e := g.ReadPublishedPage(context.Background(), guardRequest()); e != nil {
		t.Fatal("caller mutation changed captured scope", e)
	}
}

func TestGuardRefusesExpiredScopeAndUnboundedParent(t *testing.T) {
	s := guardScope()
	s.ExpiresAt = time.Now().Add(-time.Second)
	if _, e := queryread.NewGuard(ctx(t), &repository{}, s, identity); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("expired scope accepted", e)
	}
	if _, e := queryread.NewGuard(context.Background(), &repository{}, guardScope(), identity); e != queryread.ErrInvalid {
		t.Fatal("unbounded parent accepted", e)
	}
}
