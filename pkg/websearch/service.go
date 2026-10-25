package websearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrService = errors.New("websearch service unavailable")

type ApplicationDependencies struct {
	Authenticator contracts.Authenticator
	Authorizer    contracts.Authorizer
}

type Limits struct {
	MaxCount        int
	MaxContentBytes int
	Timeout         time.Duration
}

type Service struct {
	provider   Provider
	auth       contracts.Authenticator
	authorizer contracts.Authorizer
	limits     Limits
}

type SearchInput struct {
	Credential       contracts.Credential
	RequestID        contracts.ID
	Deadline         time.Time
	SpaceID          contracts.ID
	Query            string
	OriginalQuestion string
	Count            int
	NeedContent      bool
}

type FetchInput struct {
	Credential contracts.Credential
	RequestID  contracts.ID
	Deadline   time.Time
	SpaceID    contracts.ID
	URL        string
}

func NewService(provider Provider, deps ApplicationDependencies, limits Limits) (*Service, error) {
	if provider == nil || deps.Authenticator == nil || deps.Authorizer == nil {
		return nil, ErrService
	}
	if limits.MaxCount == 0 {
		limits.MaxCount = 10
	}
	if limits.MaxContentBytes == 0 {
		limits.MaxContentBytes = 1 << 20
	}
	if limits.Timeout == 0 {
		limits.Timeout = 30 * time.Second
	}
	if limits.MaxCount < 1 || limits.MaxCount > 50 || limits.MaxContentBytes < 1 || limits.MaxContentBytes > 16<<20 || limits.Timeout < time.Millisecond || limits.Timeout > time.Minute {
		return nil, ErrService
	}
	return &Service{provider: provider, auth: deps.Authenticator, authorizer: deps.Authorizer, limits: limits}, nil
}

func (s *Service) Search(ctx context.Context, in SearchInput) (SearchResponse, error) {
	if s == nil || !validInput(ctx, in.Credential, in.RequestID, in.Deadline, in.SpaceID) || !utf8.ValidString(in.Query) || utf8.RuneCountInString(in.Query) < 1 || utf8.RuneCountInString(in.Query) > 128 || strings.TrimSpace(in.Query) == "" || in.Count < 1 || in.Count > s.limits.MaxCount {
		return SearchResponse{}, ErrService
	}
	if in.OriginalQuestion != "" && (!utf8.ValidString(in.OriginalQuestion) || utf8.RuneCountInString(in.OriginalQuestion) > 128 || strings.TrimSpace(in.OriginalQuestion) == "") {
		return SearchResponse{}, ErrService
	}
	ctx, cancel := boundedContext(ctx, in.Deadline, s.limits.Timeout)
	defer cancel()
	who, scope, err := s.authorize(ctx, in.Credential, in.RequestID, in.SpaceID, "web.search", "")
	_ = who
	if err != nil {
		return SearchResponse{}, err
	}
	var original *string
	if in.OriginalQuestion != "" {
		original = &in.OriginalQuestion
	}
	out, err := s.provider.Search(ctx, SearchRequest{Query: in.Query, Count: in.Count, OriginalQuestion: original, NeedContent: in.NeedContent})
	if err != nil || ctx.Err() != nil || !validScope(scope, who, in.SpaceID, "web.search") {
		return SearchResponse{}, ErrService
	}
	for i := range out.Results {
		if len(out.Results[i].Content) > s.limits.MaxContentBytes {
			out.Results[i].Content = truncateUTF8(out.Results[i].Content, s.limits.MaxContentBytes)
		}
	}
	return out, nil
}

func (s *Service) Fetch(ctx context.Context, in FetchInput) (FetchResponse, error) {
	if s == nil || !validInput(ctx, in.Credential, in.RequestID, in.Deadline, in.SpaceID) || validateFetch(FetchRequest{URL: in.URL}) != nil {
		return FetchResponse{}, ErrService
	}
	ctx, cancel := boundedContext(ctx, in.Deadline, s.limits.Timeout)
	defer cancel()
	digest := sha256.Sum256([]byte(in.URL))
	who, scope, err := s.authorize(ctx, in.Credential, in.RequestID, in.SpaceID, "web.fetch", hex.EncodeToString(digest[:]))
	if err != nil {
		return FetchResponse{}, err
	}
	out, err := s.provider.Fetch(ctx, FetchRequest{URL: in.URL})
	if err != nil || ctx.Err() != nil || !validScope(scope, who, in.SpaceID, "web.fetch") {
		return FetchResponse{}, ErrService
	}
	if len(out.Content) > s.limits.MaxContentBytes {
		out.Content = truncateUTF8(out.Content, s.limits.MaxContentBytes)
	}
	return out, nil
}

func (s *Service) authorize(ctx context.Context, credential contracts.Credential, requestID, space contracts.ID, action contracts.Action, resource string) (contracts.Principal, contracts.AccessScope, error) {
	secret := append([]byte(nil), credential.Secret...)
	defer clear(secret)
	who, err := s.auth.Authenticate(ctx, contracts.Credential{Kind: credential.Kind, Secret: secret})
	if err != nil {
		return contracts.Principal{}, contracts.AccessScope{}, ErrService
	}
	resources := []contracts.ID(nil)
	if resource != "" {
		resources = []contracts.ID{contracts.ID(resource)}
	}
	scope, err := s.authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: action, SpaceID: space, ResourceIDs: resources})
	if err != nil || !validScope(scope, who, space, action) {
		return contracts.Principal{}, contracts.AccessScope{}, ErrService
	}
	return who, scope, nil
}

func validInput(ctx context.Context, credential contracts.Credential, requestID contracts.ID, deadline time.Time, space contracts.ID) bool {
	return ctx != nil && ctx.Err() == nil && credential.Kind != "" && len(credential.Secret) > 0 && len(credential.Secret) <= 16384 && validID(requestID) && validID(space) && !deadline.IsZero() && deadline.After(time.Now())
}

func validID(id contracts.ID) bool { return id != "" && len(id) <= 200 && utf8.ValidString(string(id)) }

func validScope(scope contracts.AccessScope, who contracts.Principal, space contracts.ID, action contracts.Action) bool {
	return scope.ScopeID != "" && scope.SubjectID == who.SubjectID && scope.Action == action && scope.SpaceID == space && scope.PolicyVersion > 0 && scope.ExpiresAt.After(time.Now())
}

func boundedContext(parent context.Context, deadline time.Time, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(parent, deadline)
	limited, stop := context.WithTimeout(ctx, timeout)
	return limited, func() { stop(); cancel() }
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (s *Service) String() string {
	return fmt.Sprintf("websearch service provider=%s", s.provider.Name())
}
