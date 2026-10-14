package queryread_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type unitFunc func(context.Context, contracts.AccessScope, func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error

func (f unitFunc) WithinRead(c context.Context, s contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	return f(c, s, cb)
}

func TestProtectedUnitClosesHandleAutomaticallyAndCollects(t *testing.T) {
	r := &repository{pages: []contracts.PublishedPage{page("a", "ra", "ga", "")}}
	u, e := queryread.ProtectUnit(unitFunc(func(c context.Context, _ contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
		return cb(c, identity, r)
	}))
	if e != nil {
		t.Fatal(e)
	}
	var escaped contracts.QueryReadRepository
	e = u.WithinRead(ctx(t), guardScope(), func(c context.Context, id contracts.QueryReadIdentity, repo contracts.QueryReadRepository) error {
		escaped = repo
		facts, e := queryread.Collect(c, repo, scope(), id, []contracts.ID{"kb"}, limits)
		if e == nil && len(facts) != 1 {
			t.Fatal("inventory lost")
		}
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := escaped.ReadPublishedPage(context.Background(), guardRequest()); e != queryread.ErrClosed || len(r.calls) != 1 {
		t.Fatal("callback handle escaped live", e)
	}
}

func TestProtectedUnitPreservesCallbackFailureEvenIfBackendIgnoresIt(t *testing.T) {
	r := &repository{}
	u, _ := queryread.ProtectUnit(unitFunc(func(c context.Context, _ contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
		_ = cb(c, identity, r)
		return nil
	}))
	failure := errors.New("application rejected snapshot")
	var escaped contracts.QueryReadRepository
	e := u.WithinRead(ctx(t), guardScope(), func(_ context.Context, _ contracts.QueryReadIdentity, r contracts.QueryReadRepository) error {
		escaped = r
		return failure
	})
	if e != failure {
		t.Fatal("callback error swallowed", e)
	}
	if _, e := escaped.LoadActiveEpoch(context.Background(), "kb"); e != queryread.ErrClosed {
		t.Fatal("failure did not close", e)
	}
}

func TestProtectedUnitRejectsSkippedOrRepeatedCallback(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		count := 0
		u, _ := queryread.ProtectUnit(unitFunc(func(c context.Context, _ contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
			if repeat {
				_ = cb(c, identity, &repository{})
				_ = cb(c, identity, &repository{})
			}
			return nil
		}))
		e := u.WithinRead(ctx(t), guardScope(), func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error {
			count++
			return nil
		})
		if e != queryread.ErrInvalid || count > 1 {
			t.Fatal("bad callback contract accepted", e, count)
		}
	}
}

func TestProtectedUnitScrubsDriverFailureAfterSuccessfulCallback(t *testing.T) {
	u, _ := queryread.ProtectUnit(unitFunc(func(c context.Context, _ contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
		if e := cb(c, identity, &repository{}); e != nil {
			return e
		}
		return errors.New("private driver detail")
	}))
	if e := u.WithinRead(ctx(t), guardScope(), func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error { return nil }); e != queryread.ErrRead {
		t.Fatal("driver failure exposed", e)
	}
}

func TestProtectedUnitFreezesScopeAndRejectsInvalidAdmission(t *testing.T) {
	r := &repository{pages: []contracts.PublishedPage{page("a", "ra", "ga", "")}}
	called := 0
	u, _ := queryread.ProtectUnit(unitFunc(func(c context.Context, s contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
		called++
		s.KnowledgeBaseIDs[0] = "other"
		return cb(c, identity, r)
	}))
	if e := u.WithinRead(ctx(t), guardScope(), func(c context.Context, _ contracts.QueryReadIdentity, r contracts.QueryReadRepository) error {
		_, e := r.ReadPublishedPage(c, guardRequest())
		return e
	}); e != nil {
		t.Fatal("driver mutation changed captured scope", e)
	}
	s := guardScope()
	s.KnowledgeBaseIDs = nil
	if e := u.WithinRead(ctx(t), s, func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error {
		t.Fatal("invalid scope admitted")
		return nil
	}); e != queryread.ErrInvalid || called != 1 {
		t.Fatal(e, called)
	}
}

func TestProtectedUnitExpiryDuringCallbackCannotSucceed(t *testing.T) {
	u, _ := queryread.ProtectUnit(unitFunc(func(c context.Context, _ contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
		return cb(c, identity, &repository{})
	}))
	s := guardScope()
	s.ExpiresAt = time.Now().Add(30 * time.Millisecond)
	e := u.WithinRead(ctx(t), s, func(c context.Context, _ contracts.QueryReadIdentity, _ contracts.QueryReadRepository) error {
		<-c.Done()
		return nil
	})
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("scope expiry became success", e)
	}
}

func TestProtectedUnitRejectsPrematureAsyncReturn(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	u, _ := queryread.ProtectUnit(unitFunc(func(c context.Context, _ contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
		go func() { _ = cb(c, identity, &repository{}); close(finished) }()
		<-entered
		return nil
	}))
	e := u.WithinRead(ctx(t), guardScope(), func(c context.Context, _ contracts.QueryReadIdentity, r contracts.QueryReadRepository) error {
		close(entered)
		<-c.Done()
		if _, err := r.ReadPublishedPage(context.Background(), guardRequest()); err == nil {
			return errors.New("invalid live handle")
		}
		return nil
	})
	if e != queryread.ErrInvalid {
		t.Fatal("async backend returned success", e)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cooperative callback did not exit")
	}
}
