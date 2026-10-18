package knowledge

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
	"testing"
)

type readLocatorStub struct{}

func (readLocatorStub) LocateReadableDocument(context.Context, contracts.Principal, contracts.ID) (contracts.ReadableDocument, error) {
	return contracts.ReadableDocument{}, ErrRead
}

type readUnitStub struct{}

func (readUnitStub) WithinRead(context.Context, contracts.AccessScope, func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	return ErrRead
}

type readContentStub struct{}

func (readContentStub) ReadFrozenContent(context.Context, contracts.FrozenContentReadRequest) (contracts.FrozenContentRecord, error) {
	return contracts.FrozenContentRecord{}, ErrRead
}

type readFactsStub struct{}

func (readFactsStub) LoadCurrentFacts(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error) {
	return queryread.ControlFacts{}, ErrRead
}

type readVerifierStub struct{}

func (readVerifierStub) VerifyReadContent(context.Context, contracts.FrozenContentReadRequest, contracts.FrozenRevisionFact, contracts.FrozenContentRecord) error {
	return nil
}
func TestConfiguredReadRuntimeRequiresClockAndDependencies(t *testing.T) {
	if _, e := NewConfiguredReadRuntime(ConfiguredReadRuntimeConfig{}); e == nil {
		t.Fatal("empty read runtime assembled")
	}
	deps := CurrentReadDependencies{Auth: runtimeAuth{}, Locator: readLocatorStub{}, Reads: readUnitStub{}, Content: readContentStub{}, Facts: readFactsStub{}, Verifier: readVerifierStub{}}
	if _, e := NewConfiguredReadRuntime(ConfiguredReadRuntimeConfig{Dependencies: deps, ClockHealth: func(context.Context) error { return ErrRead }}); e == nil {
		t.Fatal("unhealthy clock accepted")
	}
	r, e := NewConfiguredReadRuntime(ConfiguredReadRuntimeConfig{Dependencies: deps, ClockHealth: func(context.Context) error { return nil }})
	if e != nil {
		t.Fatal(e)
	}
	r.Close()
	if _, e = r.ReadCurrent(context.Background(), CurrentReadInput{}); e == nil {
		t.Fatal("closed read runtime served")
	}
}
