// Package knowledge contains transport-independent knowledge applications.
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

var ErrSearch = errors.New("knowledge search unavailable")
var ErrUnauthenticated = errors.New("knowledge authentication required")

type authenticationFailure struct{}

func (authenticationFailure) Error() string { return ErrSearch.Error() }
func (authenticationFailure) Is(target error) bool {
	return target == ErrSearch || target == ErrUnauthenticated
}

type SearchInput struct {
	Credential       contracts.Credential
	RequestID        contracts.ID
	Deadline         time.Time
	SpaceID          contracts.ID
	KnowledgeBaseIDs []contracts.ID
	Text             string
	Limit            int
}

// PlanSource is trusted application infrastructure, never a client parameter.
// It must fix current publication and resolve the configured route instance.
type PlanSource interface {
	Prepare(context.Context, contracts.Principal, contracts.RetrieveRequest) (SearchPlan, error)
}
type SearchPlan struct {
	Request     contracts.RetrieveRequest
	Expectation queryread.CurrentExpectation
	Retriever   contracts.Retriever
	Reranker    contracts.Reranker
}
type KnowledgeBaseDirectory interface {
	ListKnowledgeBases(context.Context, contracts.Principal, contracts.ID) ([]contracts.ID, error)
}

type Dependencies struct {
	KnowledgeBases KnowledgeBaseDirectory
	Auth           contracts.Authenticator
	Authorizer     contracts.Authorizer
	Plans          PlanSource
	Facts          queryread.CurrentFactsSource
	Reads          contracts.QueryReadUnit
	Backend        queryread.CurrentBackend
	Fuser          contracts.Fuser
}
type SearchService struct {
	profile []byte
	deps    Dependencies
}

func missing(v any) bool {
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
func NewSearch(s configprofile.Snapshot, d Dependencies) (*SearchService, error) {
	raw, err := configprofile.Encode(s)
	// Reflection remains explicitly unsupported until its executor is connected.
	if err != nil || s.Query.Reflection.Enabled || missing(d.Auth) || missing(d.Authorizer) || missing(d.Plans) || missing(d.Facts) || missing(d.Reads) || missing(d.Backend) {
		return nil, ErrSearch
	}
	if s.Query.Bindings[contracts.Fusion].InstanceID != "" && missing(d.Fuser) {
		return nil, ErrSearch
	}
	if len(s.Query.Routes) > 1 || !missing(d.Fuser) {
		combined, err := newMultiRoutePlans(s, d.Plans, d.Fuser)
		if err != nil {
			return nil, ErrSearch
		}
		d.Plans = combined
	}
	return &SearchService{profile: raw, deps: d}, nil
}
func copyValue[T any](v T) (T, error) {
	var out T
	if !textEncoding(reflect.ValueOf(v), 0) {
		return out, ErrSearch
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 8<<20 {
		return out, ErrSearch
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, ErrSearch
	}
	return out, nil
}
func id(v contracts.ID) bool { return v != "" && len(v) <= 200 && utf8.ValidString(string(v)) }

// Search returns evidence only after current identity, policy, publication and
// actual backend/source checks. It does not implement a network send barrier.
func (s *SearchService) Search(ctx context.Context, in SearchInput) ([]contracts.Candidate, error) {
	if s == nil || ctx == nil || ctx.Err() != nil || !id(in.RequestID) || !id(in.SpaceID) || in.Deadline.IsZero() || !in.Deadline.After(time.Now()) || strings.TrimSpace(in.Text) == "" || len(in.Text) > 16384 || !utf8.ValidString(in.Text) || in.Limit < 1 || in.Limit > 1000 || len(in.KnowledgeBaseIDs) > 256 || in.Credential.Kind == "" || len(in.Credential.Secret) == 0 || len(in.Credential.Secret) > 16384 {
		return nil, ErrSearch
	}
	profile, err := configprofile.Decode(s.profile)
	if err != nil || in.Limit > profile.Query.Policy.CandidateLimit || len(profile.Query.Routes) == 1 && in.Limit > profile.Query.Routes[0].CandidateLimit {
		return nil, ErrSearch
	}
	seen := map[contracts.ID]bool{}
	for _, kb := range in.KnowledgeBaseIDs {
		if !id(kb) || seen[kb] {
			return nil, ErrSearch
		}
		seen[kb] = true
	}
	ctx, cancel := context.WithDeadline(ctx, in.Deadline)
	defer cancel()
	ctx, limit := context.WithTimeout(ctx, 30*time.Second)
	defer limit()
	deadline, _ := ctx.Deadline()
	secret := contracts.Credential{Kind: in.Credential.Kind, Secret: append([]byte(nil), in.Credential.Secret...)}
	defer clear(secret.Secret)
	temporary := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
	who, err := s.deps.Auth.Authenticate(ctx, temporary)
	clear(temporary.Secret)
	if err != nil || ctx.Err() != nil {
		return nil, authenticationFailure{}
	}
	who, err = copyValue(who)
	if err != nil {
		return nil, ErrSearch
	}
	if in.KnowledgeBaseIDs == nil {
		if missing(s.deps.KnowledgeBases) {
			return nil, ErrSearch
		}
		actor, err := copyValue(who)
		if err != nil {
			return nil, ErrSearch
		}
		resolved, err := s.deps.KnowledgeBases.ListKnowledgeBases(ctx, actor, in.SpaceID)
		if err != nil || len(resolved) > 256 || ctx.Err() != nil {
			return nil, ErrSearch
		}
		in.KnowledgeBaseIDs = append([]contracts.ID{}, resolved...)
	}
	if len(in.KnowledgeBaseIDs) == 0 {
		temporary := contracts.Credential{Kind: secret.Kind, Secret: append([]byte(nil), secret.Secret...)}
		current, err := s.deps.Auth.Authenticate(ctx, temporary)
		clear(temporary.Secret)
		if err != nil || ctx.Err() != nil {
			return nil, ErrSearch
		}
		current, err = copyValue(current)
		if err != nil || !reflect.DeepEqual(current, who) {
			return nil, ErrSearch
		}
		return []contracts.Candidate{}, nil
	}
	seen = map[contracts.ID]bool{}
	for _, kb := range in.KnowledgeBaseIDs {
		if !id(kb) || seen[kb] {
			return nil, ErrSearch
		}
		seen[kb] = true
	}
	authRequest := contracts.AuthorizeRequest{Principal: who, Action: "knowledge.search", SpaceID: in.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), in.KnowledgeBaseIDs...)}
	authRequest, err = copyValue(authRequest)
	if err != nil {
		return nil, ErrSearch
	}
	scope, err := s.deps.Authorizer.Authorize(ctx, authRequest)
	if err != nil {
		return nil, ErrSearch
	}
	scope, err = copyValue(scope)
	if err != nil || scope.SpaceID != in.SpaceID || scope.Action != "knowledge.search" || !reflect.DeepEqual(scope.KnowledgeBaseIDs, in.KnowledgeBaseIDs) || queryread.MatchRefreshedScope(ctx, who, scope, who, scope, in.KnowledgeBaseIDs) != nil {
		return nil, ErrSearch
	}
	ctx, expire := context.WithDeadline(ctx, scope.ExpiresAt)
	defer expire()
	if scope.ExpiresAt.Before(deadline) {
		deadline = scope.ExpiresAt
	}
	candidateLimit := in.Limit
	if profile.Query.Policy.EnableRerank {
		candidateLimit = profile.Query.Policy.CandidateLimit
		if len(profile.Query.Routes) == 1 && profile.Query.Routes[0].CandidateLimit < candidateLimit {
			candidateLimit = profile.Query.Routes[0].CandidateLimit
		}
	}
	base := contracts.RetrieveRequest{Request: contracts.RequestContext{RequestID: in.RequestID, ProfileDigest: profile.Pipeline.Digest, Deadline: deadline}, Scope: scope, Query: contracts.Query{Text: in.Text}, RouteID: profile.Query.Routes[0].ID, Channels: append([]contracts.Channel(nil), profile.Query.Routes[0].Channels...), Limit: candidateLimit}
	base, err = copyValue(base)
	if err != nil {
		return nil, ErrSearch
	}
	request, err := copyValue(base)
	if err != nil {
		return nil, ErrSearch
	}
	actor, err := copyValue(who)
	if err != nil {
		return nil, ErrSearch
	}
	plan, err := s.deps.Plans.Prepare(ctx, actor, request)
	if err != nil || missing(plan.Retriever) || profile.Query.Policy.EnableRerank && missing(plan.Reranker) || ctx.Err() != nil {
		return nil, ErrSearch
	}
	fixed, err := copyValue(plan.Request)
	if err != nil || !reflect.DeepEqual(fixed.Request, base.Request) || !reflect.DeepEqual(fixed.Scope, base.Scope) || !reflect.DeepEqual(fixed.Query, base.Query) || fixed.Limit != base.Limit || fixed.RouteID != base.RouteID || !reflect.DeepEqual(fixed.Channels, base.Channels) {
		return nil, ErrSearch
	}
	expected, err := copyValue(plan.Expectation)
	if err != nil || expected.ScopeVersion != scope.PolicyVersion {
		return nil, ErrSearch
	}
	if combined, ok := plan.Retriever.(*multiRouteRetriever); ok && combined.requiresGuard {
		owned := *combined
		owned.guard = func(c context.Context, lists []contracts.RankedList) error {
			evidence := []contracts.Evidence{}
			for _, list := range lists {
				for _, candidate := range list.Candidates {
					evidence = append(evidence, contracts.Evidence{Citation: candidate.Citation, SourceExcerpt: candidate.Text, Rank: len(evidence) + 1})
				}
			}
			op := queryread.CheckOperation{Request: fixed.Request, Principal: who, Scope: scope, Expectation: expected, Evidence: evidence}
			control, err := queryread.NewAuthenticatedControl(op, secret, s.deps.Auth, s.deps.Authorizer, s.deps.Facts)
			if err != nil {
				return ErrSearch
			}
			defer control.Close()
			if queryread.CheckCurrent(c, op, control, s.deps.Reads, s.deps.Backend, queryread.CurrentLimits{MaxDocuments: 1000, MaxBytes: 4 << 20}) != nil {
				return ErrSearch
			}
			return nil
		}
		plan.Retriever = &owned
	}
	call, err := copyValue(fixed)
	if err != nil {
		return nil, ErrSearch
	}
	searchCtx, stopSearch := context.WithTimeout(ctx, time.Duration(profile.Query.Policy.RetrievalTimeoutMS)*time.Millisecond)
	candidates, err := plan.Retriever.Search(searchCtx, call)
	searchExpired := searchCtx.Err() != nil
	stopSearch()
	if err != nil || len(candidates) > candidateLimit || ctx.Err() != nil || searchExpired {
		return nil, ErrSearch
	}
	candidates, err = copyValue(candidates)
	if err != nil {
		return nil, ErrSearch
	}
	evidence := make([]contracts.Evidence, 0, len(candidates))
	size := 0
	for i, c := range candidates {
		size += len(c.Text)
		if c.Rank != i+1 || size > 4<<20 {
			return nil, ErrSearch
		}
		evidence = append(evidence, contracts.Evidence{Citation: c.Citation, SourceExcerpt: c.Text, Rank: c.Rank})
	}
	operation := queryread.CheckOperation{Request: fixed.Request, Principal: who, Scope: scope, Expectation: expected, Evidence: evidence}
	control, err := queryread.NewAuthenticatedControl(operation, secret, s.deps.Auth, s.deps.Authorizer, s.deps.Facts)
	if err != nil {
		return nil, ErrSearch
	}
	defer control.Close()
	if queryread.CheckCurrent(ctx, operation, control, s.deps.Reads, s.deps.Backend, queryread.CurrentLimits{MaxDocuments: 1000, MaxBytes: 4 << 20}) != nil || ctx.Err() != nil {
		return nil, ErrSearch
	}
	if profile.Query.Policy.EnableRerank && len(candidates) > 0 {
		// Content leaves the local retrieval boundary only after full current checks.
		input, err := copyValue(contracts.RerankRequest{Request: fixed.Request, Query: in.Text, Candidates: candidates, Limit: in.Limit})
		if err != nil {
			return nil, ErrSearch
		}
		rerankCtx, stop := context.WithTimeout(ctx, time.Duration(profile.Query.Policy.RerankTimeoutMS)*time.Millisecond)
		ranked, err := plan.Reranker.Rerank(rerankCtx, input)
		expired := rerankCtx.Err() != nil
		stop()
		if err != nil || expired || ctx.Err() != nil {
			return nil, ErrSearch
		}
		ranked, err = copyValue(ranked)
		if err != nil {
			return nil, ErrSearch
		}
		selected, err := selectReranked(candidates, ranked, in.Limit)
		if err != nil {
			return nil, err
		}
		operation.Evidence = ranked
		finalControl, err := queryread.NewAuthenticatedControl(operation, secret, s.deps.Auth, s.deps.Authorizer, s.deps.Facts)
		if err != nil {
			return nil, ErrSearch
		}
		defer finalControl.Close()
		if queryread.CheckCurrent(ctx, operation, finalControl, s.deps.Reads, s.deps.Backend, queryread.CurrentLimits{MaxDocuments: 1000, MaxBytes: 4 << 20}) != nil || ctx.Err() != nil {
			return nil, ErrSearch
		}
		candidates = selected
	}
	return candidates, nil
}

// Validate bytes before encoding/json can repair malformed provider strings.
func textEncoding(v reflect.Value, depth int) bool {
	if depth > 64 {
		return false
	}
	if v.IsValid() && v.Type() == reflect.TypeOf(time.Time{}) {
		return true
	}
	switch v.Kind() {
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Pointer, reflect.Interface:
		return v.IsNil() || textEncoding(v.Elem(), depth+1)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !textEncoding(v.Field(i), depth+1) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !textEncoding(v.Index(i), depth+1) {
				return false
			}
		}
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			if !textEncoding(it.Key(), depth+1) || !textEncoding(it.Value(), depth+1) {
				return false
			}
		}
	}
	return true
}

// ProfileIdentity exposes only the service's immutable platform profile trace.
func (s *SearchService) ProfileIdentity() (uint64, contracts.Digest, error) {
	if s == nil {
		return 0, "", ErrSearch
	}
	profile, err := configprofile.Decode(s.profile)
	if err != nil {
		return 0, "", ErrSearch
	}
	return profile.Pipeline.Version, profile.Pipeline.Digest, nil
}
