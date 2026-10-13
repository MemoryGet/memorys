package kernel_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

type parser struct{}

func (parser) Parse(context.Context, contracts.ParseRequest) (contracts.ParseResult, error) {
	return contracts.ParseResult{}, nil
}

func parserFactory(id string, closed *[]string) kernel.Factory {
	return kernel.Factory{
		Manifest: kernel.Manifest{ID: id, Version: "1.0.0", ContractVersion: "1.0.0", Capabilities: []contracts.Capability{contracts.Parsing}},
		Validate: func(json.RawMessage) error { return nil },
		Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
			return &kernel.Instance{
				Services: map[contracts.Capability]any{contracts.Parsing: parser{}},
				Health: func(context.Context) error { return nil },
				Close: func(context.Context) error { *closed = append(*closed, id); return nil },
			}, nil
		},
	}
}

func configuration(provider string) kernel.Config {
	return kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "parser", Provider: provider, Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.Parsing: "parser"}}
}

func TestReplacingProviderPreservesTypedPort(t *testing.T) {
	var closed []string
	for _, provider := range []string{"first", "second"} {
		runtime, err := kernel.Build(context.Background(), configuration(provider), []kernel.Factory{parserFactory(provider, &closed)}, kernel.Requirement{Capability: contracts.Parsing})
		if err != nil { t.Fatal(err) }
		if _, err := kernel.Resolve[contracts.Parser](runtime, contracts.Parsing); err != nil { t.Fatal(err) }
		if err := runtime.Close(context.Background()); err != nil { t.Fatal(err) }
		if err := runtime.Close(context.Background()); err != nil { t.Fatal(err) }
		if _, err := runtime.Get(contracts.Parsing); err == nil { t.Fatal("closed runtime resolved a service") }
	}
	if strings.Join(closed, ",") != "first,second" { t.Fatalf("cleanup: %v", closed) }
}

func TestInvalidConfigurationRejectedBeforeCreatingProviders(t *testing.T) {
	tests := []struct{name string; change func(*kernel.Config, *kernel.Factory)}{
		{"unknown provider", func(c *kernel.Config, _ *kernel.Factory) { c.Modules[0].Provider = "missing" }},
		{"missing binding", func(c *kernel.Config, _ *kernel.Factory) { delete(c.Bindings, contracts.Parsing) }},
		{"missing instance", func(c *kernel.Config, _ *kernel.Factory) { c.Bindings[contracts.Parsing] = "missing" }},
		{"duplicate instance", func(c *kernel.Config, _ *kernel.Factory) { c.Modules = append(c.Modules, c.Modules[0]) }},
		{"unsupported contract", func(_ *kernel.Config, f *kernel.Factory) { f.Manifest.ContractVersion = "2.0.0" }},
		{"invalid version", func(_ *kernel.Config, f *kernel.Factory) { f.Manifest.Version = "latest" }},
		{"unknown capability", func(_ *kernel.Config, f *kernel.Factory) { f.Manifest.Capabilities = append(f.Manifest.Capabilities, "mystery") }},
		{"missing dependency", func(_ *kernel.Config, f *kernel.Factory) { f.Manifest.Dependencies = []contracts.Capability{contracts.BlobStorage} }},
		{"self cycle", func(_ *kernel.Config, f *kernel.Factory) { f.Manifest.Dependencies = []contracts.Capability{contracts.Parsing} }},
		{"invalid settings", func(c *kernel.Config, _ *kernel.Factory) { c.Modules[0].Settings = json.RawMessage(`[]`) }},
	}
	for _, tc := range tests { t.Run(tc.name, func(t *testing.T) {
		var closed []string
		config, factory := configuration("parser"), parserFactory("parser", &closed)
		tc.change(&config, &factory)
		created := false
		factory.Create = func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) { created = true; return nil, nil }
		if _, err := kernel.Build(context.Background(), config, []kernel.Factory{factory}, kernel.Requirement{Capability: contracts.Parsing}); err == nil { t.Fatal("accepted invalid configuration") }
		if created { t.Fatal("provider created before preflight completed") }
	}) }
}

func TestHealthFailureClosesFailingInstance(t *testing.T) {
	var closed []string
	factory := parserFactory("parser", &closed)
	create := factory.Create
	factory.Create = func(ctx context.Context, settings json.RawMessage, deps kernel.Lookup) (*kernel.Instance, error) {
		instance, err := create(ctx, settings, deps)
		instance.Health = func(context.Context) error { return errors.New("private backend credential") }
		return instance, err
	}
	_, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory})
	if err == nil { t.Fatal("unhealthy provider was accepted") }
	if strings.Contains(err.Error(), "credential") { t.Fatal("provider details leaked through public error") }
	if len(closed) != 1 { t.Fatalf("failing instance not closed: %v", closed) }
}

func TestProviderCannotAdvertiseWrongInterface(t *testing.T) {
	var closed []string
	factory := parserFactory("parser", &closed)
	create := factory.Create
	factory.Create = func(ctx context.Context, settings json.RawMessage, deps kernel.Lookup) (*kernel.Instance, error) {
		instance, err := create(ctx, settings, deps)
		instance.Services[contracts.Parsing] = "not a Parser"
		return instance, err
	}
	if _, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory}); err == nil { t.Fatal("accepted wrong port type") }
	if len(closed) != 1 { t.Fatal("invalid instance leaked") }
}

func TestConfigurationDigestAndSnapshotsAreImmutable(t *testing.T) {
	var closed []string
	config := configuration("parser")
	runtime, err := kernel.Build(context.Background(), config, []kernel.Factory{parserFactory("parser", &closed)})
	if err != nil { t.Fatal(err) }; defer runtime.Close(context.Background())
	before := runtime.Snapshot()
	config.Bindings[contracts.Parsing] = "corrupted"
	before.Bindings[contracts.Parsing] = "corrupted"
	before.Modules[0].Capabilities[0] = "corrupted"
	after := runtime.Snapshot()
	if after.Bindings[contracts.Parsing] != "parser" || after.Modules[0].Capabilities[0] != contracts.Parsing { t.Fatal("mutable snapshot escaped") }
	if len(after.ConfigVersion) != 64 { t.Fatal("missing configuration digest") }
}

func TestRequiredFilteringCapabilitiesCannotBeOmitted(t *testing.T) {
	var closed []string
	_, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{parserFactory("parser", &closed)}, kernel.Requirement{Capability: contracts.Parsing, Features: []string{"filter.published_revision"}})
	if err == nil || len(closed) != 0 { t.Fatal("required features were not checked before activation") }
}
