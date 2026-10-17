package queryread_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type authenticateFunc func(context.Context, contracts.Credential) (contracts.Principal, error)

func (f authenticateFunc) Authenticate(c context.Context, q contracts.Credential) (contracts.Principal, error) {
	return f(c, q)
}

type authorizeFunc func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error)

func (f authorizeFunc) Authorize(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return f(c, q)
}

type controlFactsFunc func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error)

func (f controlFactsFunc) LoadCurrentFacts(c context.Context, r contracts.RequestContext, s contracts.AccessScope) (queryread.ControlFacts, error) {
	return f(c, r, s)
}

func TestAuthenticatedControlReauthenticatesAndErasesTemporaryCredentials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var log []string
	op, old, reads, backend := checkFixture(&log)
	secret := []byte("sensitive-fixture-credential")
	var scratch []byte
	authCalls, authorizeCalls, factCalls := 0, 0, 0
	auth := authenticateFunc(func(_ context.Context, c contracts.Credential) (contracts.Principal, error) {
		authCalls++
		if !bytes.Equal(c.Secret, []byte("sensitive-fixture-credential")) {
			t.Fatal("owned credential changed")
		}
		scratch = c.Secret
		return op.Principal, nil
	})
	authorize := authorizeFunc(func(_ context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
		authorizeCalls++
		if q.Principal.SubjectID != op.Principal.SubjectID || q.Action != "knowledge.search" || q.SpaceID != op.Scope.SpaceID || len(q.ResourceIDs) != 1 || q.ResourceIDs[0] != op.Expectation.Published[0].DocumentID {
			t.Fatal("authorization target not bound to saved operation")
		}
		q.KnowledgeBaseIDs[0] = "mutated"
		q.ResourceIDs[0] = "mutated"
		return old.states[0].Scope, nil
	})
	facts := controlFactsFunc(func(_ context.Context, _ contracts.RequestContext, s contracts.AccessScope) (queryread.ControlFacts, error) {
		factCalls++
		s.KnowledgeBaseIDs[0] = "mutated facts scope"
		return queryread.ControlFacts{ProfileDigest: op.Request.ProfileDigest, ReviewPolicies: map[contracts.ID]uint64{"kb": 7}}, nil
	})
	control, err := queryread.NewAuthenticatedControl(op, contracts.Credential{Kind: "bearer", Secret: secret}, auth, authorize, facts)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	secret[0] = 'X'
	if err := queryread.CheckCurrent(ctx, op, control, reads, backend, checkLimits); err != nil {
		t.Fatal(err)
	}
	if authCalls != 2 || authorizeCalls != 2 || factCalls != 2 || !bytes.Equal(scratch, make([]byte, len(scratch))) {
		t.Fatal("cached authentication or leaked argument buffer")
	}
	encoded, _ := json.Marshal(control)
	for _, s := range []string{fmt.Sprintf("%+v", control), fmt.Sprintf("%#v", control), string(encoded)} {
		if strings.Contains(s, "sensitive") || strings.Contains(s, "fixture-credential") {
			t.Fatal("credential exposed")
		}
	}
	control.Close()
	if _, err := control.Refresh(ctx, op.Principal, op.Request); err == nil || authCalls != 2 {
		t.Fatal("closed control reused credential")
	}
}

func TestAuthenticatedControlStopsOnIdentityPolicyAndFactsFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, kind := range []string{"auth", "identity", "authorize", "scope-expansion", "profile", "rules", "facts"} {
		t.Run(kind, func(t *testing.T) {
			var log []string
			op, old, _, _ := checkFixture(&log)
			z, f := 0, 0
			auth := authenticateFunc(func(context.Context, contracts.Credential) (contracts.Principal, error) {
				if kind == "auth" {
					return contracts.Principal{}, errors.New("sensitive provider cause")
				}
				who := op.Principal
				if kind == "identity" {
					who.SubjectID = "other"
				}
				return who, nil
			})
			authorize := authorizeFunc(func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error) {
				z++
				if kind == "authorize" {
					return contracts.AccessScope{}, errors.New("sensitive policy cause")
				}
				scope := old.states[0].Scope
				if kind == "scope-expansion" {
					scope.KnowledgeBaseIDs = []contracts.ID{"kb", "outside"}
				}
				return scope, nil
			})
			facts := controlFactsFunc(func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error) {
				f++
				if kind == "facts" {
					return queryread.ControlFacts{}, errors.New("sensitive database cause")
				}
				out := queryread.ControlFacts{ProfileDigest: op.Request.ProfileDigest, ReviewPolicies: map[contracts.ID]uint64{"kb": 7}}
				if kind == "profile" {
					out.ProfileDigest = contracts.Digest(strings.Repeat("b", 64))
				}
				if kind == "rules" {
					out.ReviewPolicies = nil
				}
				return out, nil
			})
			control, err := queryread.NewAuthenticatedControl(op, contracts.Credential{Kind: "bearer", Secret: []byte("credential")}, auth, authorize, facts)
			if err != nil {
				t.Fatal(err)
			}
			defer control.Close()
			if _, err := control.Refresh(ctx, op.Principal, op.Request); err == nil || strings.Contains(err.Error(), "sensitive") {
				t.Fatal("invalid current control accepted or provider cause leaked")
			}
			if (kind == "auth" || kind == "identity") && (z != 0 || f != 0) {
				t.Fatal("identity failure reached authorization")
			}
			if (kind == "authorize" || kind == "scope-expansion") && f != 0 {
				t.Fatal("authorization failure reached facts")
			}
		})
	}
}

func TestAuthenticatedControlBindsRequestAndRejectsMissingProviders(t *testing.T) {
	var log []string
	op, _, _, _ := checkFixture(&log)
	if _, err := queryread.NewAuthenticatedControl(op, contracts.Credential{Kind: "bearer", Secret: []byte("credential")}, nil, nil, nil); err == nil {
		t.Fatal("missing providers admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	calls := 0
	auth := authenticateFunc(func(context.Context, contracts.Credential) (contracts.Principal, error) {
		calls++
		return op.Principal, nil
	})
	z := authorizeFunc(func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error) { return op.Scope, nil })
	f := controlFactsFunc(func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error) {
		return queryread.ControlFacts{ProfileDigest: op.Request.ProfileDigest, ReviewPolicies: map[contracts.ID]uint64{"kb": 7}}, nil
	})
	control, err := queryread.NewAuthenticatedControl(op, contracts.Credential{Kind: "bearer", Secret: []byte("credential")}, auth, z, f)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	other := op.Request
	other.RequestID = "another-operation"
	if _, err := control.Refresh(ctx, op.Principal, other); err == nil || calls != 0 {
		t.Fatal("control rebound to different request")
	}
	if _, err := control.Refresh(context.Background(), op.Principal, op.Request); err == nil || calls != 0 {
		t.Fatal("unbounded refresh admitted")
	}
	child, stop := context.WithCancel(ctx)
	stop()
	if _, err := control.Refresh(child, op.Principal, op.Request); err == nil || calls != 0 {
		t.Fatal("cancelled refresh authenticated")
	}
}

func TestAuthenticatedControlRejectsCredentialRevokedDuringBackendWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var log []string
	op, old, reads, backend := checkFixture(&log)
	revoked := false
	authCalls, authorizeCalls := 0, 0
	auth := authenticateFunc(func(context.Context, contracts.Credential) (contracts.Principal, error) {
		authCalls++
		if revoked {
			return contracts.Principal{}, errors.New("credential revoked")
		}
		return op.Principal, nil
	})
	z := authorizeFunc(func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error) {
		authorizeCalls++
		return old.states[0].Scope, nil
	})
	f := controlFactsFunc(func(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error) {
		return queryread.ControlFacts{ProfileDigest: op.Request.ProfileDigest, ReviewPolicies: map[contracts.ID]uint64{"kb": 7}}, nil
	})
	control, err := queryread.NewAuthenticatedControl(op, contracts.Credential{Kind: "bearer", Secret: []byte("credential")}, auth, z, f)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	backend.change = func() { revoked = true }
	if queryread.CheckCurrent(ctx, op, control, reads, backend, checkLimits) == nil || authCalls != 2 || authorizeCalls != 1 {
		t.Fatal("credential revocation did not stop final refresh")
	}
}
