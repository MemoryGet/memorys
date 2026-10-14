package kernel_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

// Synthetic ports: tests prove admission and type assembly, not data behavior.
type extensionStub struct{}

func (extensionStub) Save(context.Context, contracts.MemoryWriteRequest) (contracts.MemoryRecord, error) {
	return contracts.MemoryRecord{}, nil
}
func (extensionStub) Update(context.Context, contracts.MemoryWriteRequest) (contracts.MemoryRecord, error) {
	return contracts.MemoryRecord{}, nil
}
func (extensionStub) Search(context.Context, contracts.MemorySearchRequest) ([]contracts.MemoryRecord, error) {
	return nil, nil
}
func (extensionStub) Delete(context.Context, contracts.MemoryDeleteRequest) error { return nil }
func (extensionStub) Extract(context.Context, contracts.GraphExtractRequest) (contracts.GraphResult, error) {
	return contracts.GraphResult{}, nil
}
func (extensionStub) Query(context.Context, contracts.GraphQueryRequest) (contracts.GraphResult, error) {
	return contracts.GraphResult{}, nil
}

func extensionFactory(cap contracts.Capability, provider string, features []string, created *bool) kernel.Factory {
	return kernel.Factory{Manifest: kernel.Manifest{ID: provider, Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{cap}, CapabilityFeatures: map[contracts.Capability][]string{cap: features}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
		*created = true
		return &kernel.Instance{Services: map[contracts.Capability]any{cap: extensionStub{}}, Health: func(context.Context) error { return nil }, Close: func(context.Context) error { return nil }}, nil
	}}
}
func extensionConfig(cap contracts.Capability, provider string) kernel.Config {
	return kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "extension", Provider: provider, Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{cap: "extension"}}
}

func TestExtensionMissingFeaturesRefuseBeforeCreation(t *testing.T) {
	for _, cap := range []contracts.Capability{contracts.MemoryStorage, contracts.GraphStorage} {
		features := kernel.MinimumExtensionFeatures(cap)
		for i, omitted := range features {
			t.Run(string(cap)+"/"+omitted, func(t *testing.T) {
				partial := append([]string(nil), features[:i]...)
				partial = append(partial, features[i+1:]...)
				created := false
				_, err := kernel.Build(context.Background(), extensionConfig(cap, "extension.provider"), []kernel.Factory{extensionFactory(cap, "extension.provider", partial, &created)})
				if err == nil || created {
					t.Fatal("missing minimum feature admitted", err, created)
				}
			})
		}
	}
}

func TestExtensionTwoDeclaredImplementationsAssembleSamePort(t *testing.T) {
	for _, cap := range []contracts.Capability{contracts.MemoryStorage, contracts.GraphStorage} {
		for _, provider := range []string{"extension.first", "extension.second"} {
			created := false
			config := extensionConfig(cap, provider)
			factories := []kernel.Factory{extensionFactory(cap, provider, kernel.MinimumExtensionFeatures(cap), &created)}
			if cap == contracts.GraphStorage {
				config.Modules = append(config.Modules, kernel.ModuleConfig{ID: "manager", Provider: "graph.manager", Settings: json.RawMessage(`{}`)})
				config.Bindings[contracts.GraphProjectionControl] = "manager"
				factories = append(factories, graphManagerFactory())
			}
			r, err := kernel.Build(context.Background(), config, factories)
			if err != nil || !created {
				t.Fatal(err, created)
			}
			service, err := r.Get(cap)
			if err != nil {
				t.Fatal(err)
			}
			if cap == contracts.MemoryStorage {
				if _, ok := service.(contracts.Memory); !ok {
					t.Fatal("memory port missing")
				}
			}
			if cap == contracts.GraphStorage {
				if _, ok := service.(contracts.Graph); !ok {
					t.Fatal("graph port missing")
				}
			}
			if err := r.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestExtensionMinimumPolicyCannotEscapeAsMutableSlice(t *testing.T) {
	before := kernel.MinimumExtensionFeatures(contracts.MemoryStorage)
	before[0] = "corrupted"
	if kernel.MinimumExtensionFeatures(contracts.MemoryStorage)[0] == "corrupted" {
		t.Fatal("policy escaped")
	}
	if len(kernel.MinimumExtensionFeatures(contracts.Parsing)) != 0 {
		t.Fatal("optional policy applied to base parser")
	}
}
