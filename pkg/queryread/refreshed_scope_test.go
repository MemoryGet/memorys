package queryread_test

import (
	"context"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func refreshPrincipal() contracts.Principal {
	return contracts.Principal{SubjectID: "subject", SubjectType: "service", Issuer: "identity", ClientID: "agent", Delegation: &contracts.Delegation{UserID: "user", ClientID: "agent", ProofReference: "verified-proof"}}
}

func TestRefreshedScopeRejectsIdentityAndScopeSubstitution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cases := map[string]func(*contracts.Principal, *contracts.AccessScope){
		"actor":              func(p *contracts.Principal, s *contracts.AccessScope) { p.SubjectID = "other"; s.SubjectID = "other" },
		"issuer":             func(p *contracts.Principal, s *contracts.AccessScope) { p.Issuer = "other" },
		"type":               func(p *contracts.Principal, s *contracts.AccessScope) { p.SubjectType = "user" },
		"client":             func(p *contracts.Principal, s *contracts.AccessScope) { p.ClientID = "other" },
		"delegation removed": func(p *contracts.Principal, s *contracts.AccessScope) { p.Delegation = nil },
		"delegation user":    func(p *contracts.Principal, s *contracts.AccessScope) { p.Delegation.UserID = "other" },
		"delegation client":  func(p *contracts.Principal, s *contracts.AccessScope) { p.Delegation.ClientID = "other" },
		"space":              func(p *contracts.Principal, s *contracts.AccessScope) { s.SpaceID = "other" },
		"policy":             func(p *contracts.Principal, s *contracts.AccessScope) { s.PolicyVersion++ },
		"range expanded": func(p *contracts.Principal, s *contracts.AccessScope) {
			s.KnowledgeBaseIDs = []contracts.ID{"kb", "other"}
		},
		"range excludes source": func(p *contracts.Principal, s *contracts.AccessScope) { s.KnowledgeBaseIDs = []contracts.ID{"other"} },
		"expired":               func(p *contracts.Principal, s *contracts.AccessScope) { s.ExpiresAt = time.Now().Add(-time.Second) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			original, fresh := refreshPrincipal(), refreshPrincipal()
			before, current := currentScope(), currentScope()
			change(&fresh, &current)
			if queryread.MatchRefreshedScope(ctx, original, before, fresh, current, []contracts.ID{"kb"}) != queryread.ErrStale {
				t.Fatal("changed operation accepted")
			}
		})
	}
}

func TestRefreshedScopeAllowsRotationAndNarrowingIncludingEmptyEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, b := refreshPrincipal(), refreshPrincipal()
	old, current := currentScope(), currentScope()
	old.KnowledgeBaseIDs = []contracts.ID{"kb", "extra"}
	old.ExpiresAt = time.Now().Add(-time.Second) // Auth module can refresh expiry.
	current.ScopeID = "new-scope"
	b.Delegation.ProofReference = "new-verified-proof"
	for _, required := range [][]contracts.ID{{"kb"}, nil} {
		if err := queryread.MatchRefreshedScope(ctx, a, old, b, current, required); err != nil {
			t.Fatal(err)
		}
	}
	if queryread.MatchRefreshedScope(ctx, a, old, b, current, []contracts.ID{"extra"}) != queryread.ErrStale {
		t.Fatal("source removed from narrowed scope accepted")
	}
}

func TestRefreshedScopeRejectsMalformedAndUnboundedInputs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, mutate := range []func(*contracts.Principal, *contracts.AccessScope){
		func(p *contracts.Principal, s *contracts.AccessScope) { p.Delegation.ProofReference = "" },
		func(p *contracts.Principal, s *contracts.AccessScope) { s.SubjectID = "not-principal" },
		func(p *contracts.Principal, s *contracts.AccessScope) { s.Action = "publication.commit" },
		func(p *contracts.Principal, s *contracts.AccessScope) { s.KnowledgeBaseIDs = nil },
		func(p *contracts.Principal, s *contracts.AccessScope) {
			s.KnowledgeBaseIDs = []contracts.ID{"kb", "kb"}
		},
		func(p *contracts.Principal, s *contracts.AccessScope) { p.Issuer = string([]byte{0xff}) },
	} {
		a, b := refreshPrincipal(), refreshPrincipal()
		old, current := currentScope(), currentScope()
		mutate(&b, &current)
		if queryread.MatchRefreshedScope(ctx, a, old, b, current, nil) != queryread.ErrInvalid {
			t.Fatal("invalid refresh accepted")
		}
	}
	for _, required := range [][]contracts.ID{{"kb", "kb"}, {"unrequested"}, {""}} {
		if queryread.MatchRefreshedScope(ctx, refreshPrincipal(), currentScope(), refreshPrincipal(), currentScope(), required) != queryread.ErrInvalid {
			t.Fatal("invalid required set accepted")
		}
	}
	for _, c := range []context.Context{nil, context.Background()} {
		if queryread.MatchRefreshedScope(c, refreshPrincipal(), currentScope(), refreshPrincipal(), currentScope(), nil) != queryread.ErrInvalid {
			t.Fatal("unbounded refresh accepted")
		}
	}
	cancel()
	if queryread.MatchRefreshedScope(ctx, refreshPrincipal(), currentScope(), refreshPrincipal(), currentScope(), nil) != context.Canceled {
		t.Fatal("cancellation ignored")
	}
}
