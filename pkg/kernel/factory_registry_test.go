package kernel_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

func TestFactoryRegistryRejectsDuplicateReleasesAndResolvesPinnedProvider(t *testing.T) {
	factory := func(provider, version string) kernel.Factory {
		return kernel.Factory{Manifest: kernel.Manifest{ID: provider, Version: version, ContractVersion: "1.0.0", Capabilities: []contracts.Capability{contracts.Parsing}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) { return nil, nil }}
	}
	if _, err := kernel.NewFactoryRegistry([]kernel.Factory{factory("parser", "1.0.0"), factory("parser", "1.0.0")}); !errors.Is(err, kernel.ErrFactoryRegistry) {
		t.Fatal("duplicate provider release accepted", err)
	}
	registry, err := kernel.NewFactoryRegistry([]kernel.Factory{factory("parser", "1.0.0"), factory("parser", "2.0.0")})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.Resolve(context.Background(), kernel.ModuleConfig{ID: "instance", Provider: "parser", ImplementationVersion: "2.0.0", Settings: json.RawMessage(`{}`)})
	if err != nil || resolved.Manifest.Version != "2.0.0" {
		t.Fatal("pinned provider release did not resolve", resolved.Manifest.Version, err)
	}
	if _, err = registry.Resolve(context.Background(), kernel.ModuleConfig{ID: "instance", Provider: "missing", Settings: json.RawMessage(`{}`)}); !errors.Is(err, kernel.ErrFactoryRegistry) {
		t.Fatal("unknown provider resolved", err)
	}
}
