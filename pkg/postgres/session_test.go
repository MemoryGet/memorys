package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func testScope() contracts.AccessScope {
	return contracts.AccessScope{ScopeID: "scope", SubjectID: "actor", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "publication.history.read", PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}
}
func TestSessionRejectsRebindingWiderBudgetAndClosedHandles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	scope := testScope()
	s := &session{ctx: ctx, scope: cloneScope(scope), verify: func(context.Context, contracts.AccessScope) error { return nil }}
	if s.enter(ctx, scope) != nil {
		t.Fatal("bound context rejected")
	}
	s.mu.Unlock()
	wrong := scope
	wrong.SubjectID = "other"
	if s.enter(ctx, wrong) == nil {
		t.Fatal("subject rebinding")
	}
	if s.enter(context.Background(), scope) == nil {
		t.Fatal("unbounded context")
	}
	wider, stop := context.WithTimeout(context.Background(), 2*time.Minute)
	defer stop()
	if s.enter(wider, scope) == nil {
		t.Fatal("budget expanded")
	}
	s.closed = true
	if s.enter(ctx, scope) == nil {
		t.Fatal("closed reference usable")
	}
}
func TestVerifierPanicDoesNotKeepSessionMutexLocked(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	scope := testScope()
	s := &session{ctx: ctx, scope: scope, verify: func(context.Context, contracts.AccessScope) error { panic("fixture panic") }}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		s.enter(ctx, scope)
	}()
	s.verify = func(context.Context, contracts.AccessScope) error { return nil }
	finished := make(chan bool, 1)
	go func() {
		err := s.enter(ctx, scope)
		if err == nil {
			s.mu.Unlock()
		}
		finished <- err == nil
	}()
	select {
	case ok := <-finished:
		if !ok {
			t.Fatal("mutex retained")
		}
	case <-time.After(time.Second):
		t.Fatal("panic leaked mutex lock")
	}
}
func TestPrivateActorCodecPreservesDelegation(t *testing.T) {
	p := contracts.Principal{SubjectID: "service", SubjectType: "service", Issuer: "verified-issuer", ClientID: "client", Delegation: &contracts.Delegation{UserID: "user", ClientID: "client", ProofReference: "opaque-reference"}}
	raw, err := encodeActor(p)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeActor(raw)
	if err != nil || !reflect.DeepEqual(decoded, p) {
		t.Fatal("actor mapping lost data", err)
	}
}

func TestScopeVerificationCannotReviveCancelledOperation(t *testing.T) {
	parent, stopParent := context.WithTimeout(context.Background(), time.Minute)
	defer stopParent()
	for _, cancelParent := range []bool{false, true} {
		operation, stop := context.WithCancel(parent)
		scope := testScope()
		s := &session{ctx: parent, scope: scope, verify: func(context.Context, contracts.AccessScope) error {
			if cancelParent {
				stopParent()
			} else {
				stop()
			}
			return nil
		}}
		if s.enter(operation, scope) == nil {
			s.mu.Unlock()
			t.Fatal("verification success revived cancelled context")
		}
		stop()
	}
}
