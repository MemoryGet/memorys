package queryread

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// CurrentFactsSource must read current authoritative configuration and review
// rules for the newly authorized scope, never echo the requested digest or use
// an unvalidated client snapshot. Concrete identity/policy/config providers are
// required deployment dependencies of AuthenticatedControl.
type CurrentFactsSource interface {
	LoadCurrentFacts(context.Context, contracts.RequestContext, contracts.AccessScope) (ControlFacts, error)
}
type ControlFacts struct {
	ProfileDigest  contracts.Digest
	ReviewPolicies map[contracts.ID]uint64
}

// AuthenticatedControl is bound to one prepared operation. Every Refresh calls
// both authentication and current authorization, then loads current facts.
// Credentials remain private and are erased by Close; no provider SDK types
// enter the orchestration interface.
type AuthenticatedControl struct {
	mu         sync.Mutex
	closed     bool
	operation  CheckOperation
	credential contracts.Credential
	auth       contracts.Authenticator
	authorizer contracts.Authorizer
	facts      CurrentFactsSource
	required   []contracts.ID
	resources  []contracts.ID
}

var _ CurrentControl = (*AuthenticatedControl)(nil)

func missingControlProvider(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}

func NewAuthenticatedControl(op CheckOperation, credential contracts.Credential, auth contracts.Authenticator, authorizer contracts.Authorizer, facts CurrentFactsSource) (*AuthenticatedControl, error) {
	if missingControlProvider(auth) || missingControlProvider(authorizer) || missingControlProvider(facts) || credential.Kind == "" || len(credential.Kind) > 200 || !utf8.ValidString(credential.Kind) || len(credential.Secret) == 0 || len(credential.Secret) > 16<<10 || op.Request.RequestID == "" || !reviewDigest(op.Request.ProfileDigest) || op.Request.Deadline.IsZero() || !principalShape(op.Principal) || op.Expectation.ScopeVersion != op.Scope.PolicyVersion {
		return nil, ErrInvalid
	}
	if _, ok := scopeKBs(op.Scope, op.Principal); !ok {
		return nil, ErrInvalid
	}
	fixed, err := copyCheckValue(op)
	if err != nil || bindCheckEvidence(fixed) != nil {
		return nil, ErrInvalid
	}
	p := &AuthenticatedControl{operation: fixed, credential: contracts.Credential{Kind: credential.Kind, Secret: append([]byte(nil), credential.Secret...)}, auth: auth, authorizer: authorizer, facts: facts}
	seenKB, seenDoc := map[contracts.ID]bool{}, map[contracts.ID]bool{}
	allowed := map[contracts.ID]bool{}
	for _, kb := range fixed.Scope.KnowledgeBaseIDs {
		allowed[kb] = true
	}
	for _, f := range fixed.Expectation.Published {
		if f.DocumentID == "" || seenDoc[f.DocumentID] || f.SpaceID != fixed.Scope.SpaceID || !allowed[f.KnowledgeBaseID] {
			p.Close()
			return nil, ErrInvalid
		}
		seenDoc[f.DocumentID] = true
		p.resources = append(p.resources, f.DocumentID)
		if !seenKB[f.KnowledgeBaseID] {
			seenKB[f.KnowledgeBaseID] = true
			p.required = append(p.required, f.KnowledgeBaseID)
		}
	}
	return p, nil
}

func (p *AuthenticatedControl) Refresh(ctx context.Context, who contracts.Principal, r contracts.RequestContext) (CurrentControlState, error) {
	if p == nil || ctx == nil {
		return CurrentControlState{}, ErrCheck
	}
	if _, ok := ctx.Deadline(); !ok {
		return CurrentControlState{}, ErrInvalid
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || !reflect.DeepEqual(who, p.operation.Principal) || r.RequestID != p.operation.Request.RequestID || r.ProfileDigest != p.operation.Request.ProfileDigest || !r.Deadline.Equal(p.operation.Request.Deadline) {
		return CurrentControlState{}, ErrCheck
	}
	ctx, cancel := context.WithDeadline(ctx, p.operation.Request.Deadline)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, p.operation.Scope.ExpiresAt)
	defer expire()
	if ctx.Err() != nil {
		return CurrentControlState{}, ctx.Err()
	}
	credential := contracts.Credential{Kind: p.credential.Kind, Secret: append([]byte(nil), p.credential.Secret...)}
	defer clear(credential.Secret)
	actual, err := p.auth.Authenticate(ctx, credential)
	// Erase the temporary argument copy once authentication finishes.
	clear(credential.Secret)
	if err != nil || ctx.Err() != nil {
		return CurrentControlState{}, checkFailure(ctx)
	}
	actual, err = copyCheckValue(actual)
	if err != nil {
		return CurrentControlState{}, ErrCheck
	}
	if MatchRefreshedScope(ctx, p.operation.Principal, p.operation.Scope, actual, p.operation.Scope, p.required) != nil {
		return CurrentControlState{}, ErrStale
	}
	authorize := contracts.AuthorizeRequest{Principal: actual, Action: p.operation.Scope.Action, SpaceID: p.operation.Scope.SpaceID, KnowledgeBaseIDs: p.operation.Scope.KnowledgeBaseIDs, ResourceIDs: p.resources}
	authorize, err = copyCheckValue(authorize)
	if err != nil {
		return CurrentControlState{}, ErrCheck
	}
	scope, err := p.authorizer.Authorize(ctx, authorize)
	if err != nil || ctx.Err() != nil {
		return CurrentControlState{}, checkFailure(ctx)
	}
	scope, err = copyCheckValue(scope)
	if err != nil {
		return CurrentControlState{}, ErrCheck
	}
	if err := MatchRefreshedScope(ctx, p.operation.Principal, p.operation.Scope, actual, scope, p.required); err != nil {
		return CurrentControlState{}, err
	}
	factsScope, err := copyCheckValue(scope)
	if err != nil {
		return CurrentControlState{}, ErrCheck
	}
	facts, err := p.facts.LoadCurrentFacts(ctx, r, factsScope)
	if err != nil || ctx.Err() != nil {
		return CurrentControlState{}, checkFailure(ctx)
	}
	facts, err = copyCheckValue(facts)
	if err != nil {
		return CurrentControlState{}, ErrCheck
	}
	if facts.ProfileDigest != p.operation.Request.ProfileDigest {
		return CurrentControlState{}, ErrStale
	}
	for _, kb := range p.required {
		if facts.ReviewPolicies[kb] == 0 {
			return CurrentControlState{}, ErrCheck
		}
	}
	return CurrentControlState{Principal: actual, Scope: scope, ProfileDigest: facts.ProfileDigest, ReviewPolicies: facts.ReviewPolicies}, ctx.Err()
}

func (p *AuthenticatedControl) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.credential.Secret)
	p.credential = contracts.Credential{}
	p.closed = true
}
func (*AuthenticatedControl) String() string   { return "AuthenticatedControl{credential:redacted}" }
func (*AuthenticatedControl) GoString() string { return "AuthenticatedControl{credential:redacted}" }
func (*AuthenticatedControl) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"kind": "authenticated_current_control"})
}
