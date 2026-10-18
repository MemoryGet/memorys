package knowledge

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
	"testing"
)

func TestConfiguredRuntimeRequiresTrustedDependenciesAndCloses(t *testing.T) {
	p := configuredProfile(t)
	if _, e := NewConfiguredRuntime(ConfiguredRuntimeConfig{Profile: p}); e == nil {
		t.Fatal("incomplete runtime assembled")
	}
	r, e := NewConfiguredRuntime(ConfiguredRuntimeConfig{Profile: p, Dependencies: Dependencies{Auth: runtimeAuth{}, Authorizer: configuredAuth{}, Facts: configuredFacts{}, Reads: configuredReads{}, Backend: configuredBackend{}}, Limits: queryread.Limits{PageSize: 1, MaxPages: 1, MaxFacts: 1, MaxGenerations: 1}, ClockHealth: func(context.Context) error { return nil }, Resolve: func(context.Context, contracts.ModuleBinding) (contracts.Retriever, error) {
		return configuredRetriever{}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	r.Close()
	if _, e = r.Search(context.Background(), SearchInput{}); e == nil {
		t.Fatal("closed runtime searched")
	}
}

type configuredBackend struct{}

type runtimeAuth struct{}

func (runtimeAuth) Authenticate(context.Context, contracts.Credential) (contracts.Principal, error) {
	return contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "issuer", ClientID: "client"}, nil
}

func (configuredBackend) Verify(context.Context, contracts.RequestContext, contracts.AccessScope, queryread.CurrentObservation, []contracts.Evidence) error {
	return nil
}

type configuredRetriever struct{}

func (configuredRetriever) Search(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	return nil, nil
}
