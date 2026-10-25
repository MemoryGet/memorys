package websearch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type serviceProvider struct {
	search SearchResponse
	fetch  FetchResponse
}

func (p serviceProvider) Name() string { return "fake" }
func (p serviceProvider) Search(context.Context, SearchRequest) (SearchResponse, error) {
	return p.search, nil
}
func (p serviceProvider) Fetch(context.Context, FetchRequest) (FetchResponse, error) {
	return p.fetch, nil
}

type serviceAuth struct{}

func (serviceAuth) Authenticate(context.Context, contracts.Credential) (contracts.Principal, error) {
	return contracts.Principal{SubjectID: "subject"}, nil
}

type serviceAuthorizer struct{}

func (serviceAuthorizer) Authorize(_ context.Context, in contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return contracts.AccessScope{ScopeID: "scope", SubjectID: in.Principal.SubjectID, Action: in.Action, SpaceID: in.SpaceID, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func serviceFixture(t *testing.T, provider Provider, limits Limits) *Service {
	t.Helper()
	s, err := NewService(provider, ApplicationDependencies{Authenticator: serviceAuth{}, Authorizer: serviceAuthorizer{}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func serviceInput() SearchInput {
	return SearchInput{Credential: contracts.Credential{Kind: "bearer", Secret: []byte("secret")}, RequestID: "request", Deadline: time.Now().Add(time.Minute), SpaceID: "space", Query: "query", Count: 1}
}

func TestServiceAuthorizesAndTruncatesProviderContent(t *testing.T) {
	s := serviceFixture(t, serviceProvider{search: SearchResponse{Results: []SearchResult{{Title: "title", Content: "你好世界"}}}}, Limits{MaxCount: 1, MaxContentBytes: 7, Timeout: time.Second})
	out, err := s.Search(context.Background(), serviceInput())
	if err != nil {
		t.Fatal(err)
	}
	if out.Results[0].Content != "你好" {
		t.Fatalf("content=%q", out.Results[0].Content)
	}
}

func TestServiceRejectsExpiredAndUnauthorizedInput(t *testing.T) {
	s := serviceFixture(t, serviceProvider{}, Limits{})
	in := serviceInput()
	in.Deadline = time.Now().Add(-time.Second)
	if _, err := s.Search(context.Background(), in); !errors.Is(err, ErrService) {
		t.Fatalf("err=%v", err)
	}
	in = serviceInput()
	in.Credential.Secret = nil
	if _, err := s.Search(context.Background(), in); !errors.Is(err, ErrService) {
		t.Fatalf("err=%v", err)
	}
}

func TestServiceFetchUsesWebFetchAction(t *testing.T) {
	s := serviceFixture(t, serviceProvider{fetch: FetchResponse{Title: "title", Content: "body"}}, Limits{})
	out, err := s.Fetch(context.Background(), FetchInput{Credential: contracts.Credential{Kind: "bearer", Secret: []byte("secret")}, RequestID: "request", Deadline: time.Now().Add(time.Minute), SpaceID: "space", URL: "https://example.com"})
	if err != nil || out.Title != "title" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
