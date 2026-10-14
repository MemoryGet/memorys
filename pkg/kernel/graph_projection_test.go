package kernel_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

type graphControlStub struct{}

func graphManagerFactory() kernel.Factory {
	return kernel.Factory{Manifest: kernel.Manifest{ID: "graph.manager", Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.GraphProjectionControl}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
		return &kernel.Instance{Services: map[contracts.Capability]any{contracts.GraphProjectionControl: graphControlStub{}}, Health: func(context.Context) error { return nil }, Close: func(context.Context) error { return nil }}, nil
	}}
}

func (graphControlStub) Prepare(context.Context, contracts.GraphProjectionPrepareRequest) (contracts.GraphProjectionProof, error) {
	return contracts.GraphProjectionProof{}, nil
}
func (graphControlStub) Verify(context.Context, contracts.GraphProjectionVerifyRequest) (contracts.GraphProjectionProof, error) {
	return contracts.GraphProjectionProof{}, nil
}
func (graphControlStub) InvalidateSource(context.Context, contracts.GraphSourceInvalidationRequest) (contracts.GraphCleanupReceipt, error) {
	return contracts.GraphCleanupReceipt{}, nil
}
func (graphControlStub) LoadCleanup(context.Context, contracts.GraphCleanupLoadRequest) (contracts.GraphCleanupReceipt, error) {
	return contracts.GraphCleanupReceipt{}, nil
}
func (graphControlStub) DeleteGeneration(context.Context, contracts.GraphGenerationDeleteRequest) (contracts.GraphCleanupReceipt, error) {
	return contracts.GraphCleanupReceipt{}, nil
}

func TestGraphLifecycleUsesDistinctTypedPort(t *testing.T) {
	for _, provider := range []string{"graph.control.first", "graph.control.second"} {
		cap := contracts.GraphProjectionControl
		factory := kernel.Factory{Manifest: kernel.Manifest{ID: provider, Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{cap}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
			return &kernel.Instance{Services: map[contracts.Capability]any{cap: graphControlStub{}}, Health: func(context.Context) error { return nil }, Close: func(context.Context) error { return nil }}, nil
		}}
		r, err := kernel.Build(context.Background(), extensionConfig(cap, provider), []kernel.Factory{factory})
		if err != nil {
			t.Fatal(err)
		}
		service, err := r.Get(cap)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := service.(contracts.GraphProjectionManager); !ok {
			t.Fatal("management port missing")
		}
		if err := r.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGraphQueryPortCannotPretendToManageProjections(t *testing.T) {
	cap := contracts.GraphProjectionControl
	closed := false
	factory := kernel.Factory{Manifest: kernel.Manifest{ID: "graph.control", Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{cap}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
		return &kernel.Instance{Services: map[contracts.Capability]any{cap: extensionStub{}}, Health: func(context.Context) error { return nil }, Close: func(context.Context) error { closed = true; return nil }}, nil
	}}
	if _, err := kernel.Build(context.Background(), extensionConfig(cap, "graph.control"), []kernel.Factory{factory}); err == nil || !closed {
		t.Fatal("query-only port admitted or not closed", err, closed)
	}
}

func TestGraphQueryRequiresManagementBeforeAnyCreation(t *testing.T) {
	created := false
	cap := contracts.GraphStorage
	factory := extensionFactory(cap, "graph.query", kernel.MinimumExtensionFeatures(cap), &created)
	if _, err := kernel.Build(context.Background(), extensionConfig(cap, "graph.query"), []kernel.Factory{factory}); err == nil || created {
		t.Fatal("graph query without management admitted", err, created)
	}
}

func TestGraphManagementCanBeBoundPerInstance(t *testing.T) {
	created := false
	cap := contracts.GraphStorage
	factory := extensionFactory(cap, "graph.query", kernel.MinimumExtensionFeatures(cap), &created)
	create := factory.Create
	factory.Create = func(ctx context.Context, raw json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
		control, err := lookup("projection_manager")
		if err != nil {
			return nil, err
		}
		if _, ok := control.(contracts.GraphProjectionManager); !ok {
			t.Fatal("wrong graph companion port")
		}
		return create(ctx, raw, lookup)
	}
	config := extensionConfig(cap, "graph.query")
	config.Modules[0].DependencyBindings = map[string]string{"projection_manager": "manager"}
	config.Modules = append(config.Modules, kernel.ModuleConfig{ID: "manager", Provider: "graph.manager", Settings: json.RawMessage(`{}`)})
	r, err := kernel.Build(context.Background(), config, []kernel.Factory{factory, graphManagerFactory()})
	if err != nil || !created {
		t.Fatal(err, created)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCompositeGraphManagerDoesNotCreateSelfDependency(t *testing.T) {
	created := false
	cap := contracts.GraphStorage
	factory := extensionFactory(cap, "graph.composite", kernel.MinimumExtensionFeatures(cap), &created)
	factory.Manifest.Capabilities = append(factory.Manifest.Capabilities, contracts.GraphProjectionControl)
	create := factory.Create
	factory.Create = func(ctx context.Context, raw json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
		instance, err := create(ctx, raw, lookup)
		if err == nil {
			instance.Services[contracts.GraphProjectionControl] = graphControlStub{}
		}
		return instance, err
	}
	r, err := kernel.Build(context.Background(), extensionConfig(cap, "graph.composite"), []kernel.Factory{factory})
	if err != nil || !created {
		t.Fatal(err, created)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
