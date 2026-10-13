package kernel_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

type compositeEngine struct{}

func (compositeEngine) PrepareProjection(context.Context, contracts.CompositePrepareRequest) (contracts.ReadinessProof, error) {
	return contracts.ReadinessProof{}, nil
}
func (compositeEngine) Verify(context.Context, contracts.ReadinessProof) error     { return nil }
func (compositeEngine) Delete(context.Context, contracts.IndexDeleteRequest) error { return nil }
func (compositeEngine) Search(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	return nil, nil
}

func TestCompositeProviderReplacementKeepsOneTypedPort(t *testing.T) {
	for _, provider := range []string{"engine-a", "engine-b"} {
		closed := false
		factory := kernel.Factory{Manifest: kernel.Manifest{ID: provider, Version: "0.1.0", ContractVersion: "1.0",
			Capabilities: []contracts.Capability{contracts.CompositeRetrieval}}, Validate: func(json.RawMessage) error { return nil },
			Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
				return &kernel.Instance{Services: map[contracts.Capability]any{contracts.CompositeRetrieval: compositeEngine{}},
					Health: func(context.Context) error { return nil }, Close: func(context.Context) error { closed = true; return nil }}, nil
			}}
		config := kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "engine", Provider: provider}},
			Bindings: map[contracts.Capability]string{contracts.CompositeRetrieval: "engine"}}
		runtime, err := kernel.Build(context.Background(), config, []kernel.Factory{factory})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := kernel.Resolve[contracts.CompositeEngine](runtime, contracts.CompositeRetrieval); err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Get(contracts.IndexWriting); err == nil {
			t.Fatal("composite invented independently replaceable indexing")
		}
		if err := runtime.Close(context.Background()); err != nil || !closed {
			t.Fatal("composite was not closed")
		}
	}
}

func TestPartialServiceCannotSatisfyCompositeCapability(t *testing.T) {
	var closed []string
	factory := parserFactory("parser", &closed)
	factory.Manifest.Capabilities = []contracts.Capability{contracts.CompositeRetrieval}
	original := factory.Create
	factory.Create = func(ctx context.Context, settings json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
		instance, err := original(ctx, settings, lookup)
		instance.Services = map[contracts.Capability]any{contracts.CompositeRetrieval: parser{}}
		return instance, err
	}
	config := configuration("parser")
	config.Bindings = map[contracts.Capability]string{contracts.CompositeRetrieval: "parser"}
	if _, err := kernel.Build(context.Background(), config, []kernel.Factory{factory}); err == nil || len(closed) != 1 {
		t.Fatal("partial composite accepted or leaked")
	}
}
