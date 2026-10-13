package kernel_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

func TestExplicitDependencySlotsAndReverseCleanup(t *testing.T) {
	var closed, created []string
	base, child := parserFactory("base", &closed), parserFactory("child", &closed)
	base.Manifest.CapabilityFeatures = map[contracts.Capability][]string{contracts.Parsing: {"format.text"}}
	child.Manifest.DependencySlots = []kernel.Dependency{{Slot: "source", Capability: contracts.Parsing, ContractVersion: "1.0", Features: []string{"format.text"}}}
	for _, factory := range []*kernel.Factory{&base, &child} {
		original, name := factory.Create, factory.Manifest.ID
		factory.Create = func(ctx context.Context, settings json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
			if name == "child" {
				if value, err := lookup("source"); err != nil {
					return nil, err
				} else if _, ok := value.(contracts.Parser); !ok {
					return nil, errors.New("wrong dependency")
				}
				if _, err := lookup("not-declared"); err == nil {
					return nil, errors.New("dependency lookup escaped its declaration")
				}
			}
			created = append(created, name)
			return original(ctx, settings, lookup)
		}
	}
	config := kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "z-child", Provider: "child", Settings: json.RawMessage(`{}`), DependencyBindings: map[string]string{"source": "a-base"}},
		{ID: "a-base", Provider: "base", Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.Parsing: "z-child"}}
	runtime, err := kernel.Build(context.Background(), config, []kernel.Factory{child, base})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(created, ",") != "base,child" || strings.Join(closed, ",") != "child,base" {
		t.Fatalf("creation/cleanup order: %v %v", created, closed)
	}
}

func TestPartialCreationErrorIsClosedWithoutLeakingCause(t *testing.T) {
	var closed []string
	factory := parserFactory("parser", &closed)
	original := factory.Create
	factory.Create = func(ctx context.Context, settings json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
		instance, _ := original(ctx, settings, lookup)
		return instance, errors.New("private-token-material")
	}
	_, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory})
	if err == nil || strings.Contains(err.Error(), "private-token-material") || len(closed) != 1 {
		t.Fatalf("unsafe failure cleanup: %v %v", err, closed)
	}
}

func TestHealthPanicIsRejectedAndClosed(t *testing.T) {
	var closed []string
	factory := parserFactory("parser", &closed)
	original := factory.Create
	factory.Create = func(ctx context.Context, settings json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
		instance, err := original(ctx, settings, lookup)
		instance.Health = func(context.Context) error { panic("private-token-material") }
		return instance, err
	}
	_, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory})
	if err == nil || strings.Contains(err.Error(), "private-token-material") || len(closed) != 1 {
		t.Fatalf("panic escaped or resource leaked: %v %v", err, closed)
	}
}

func TestTypedNilCannotSatisfyDeclaredPort(t *testing.T) {
	var closed []string
	factory := parserFactory("parser", &closed)
	original := factory.Create
	factory.Create = func(ctx context.Context, settings json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
		instance, err := original(ctx, settings, lookup)
		instance.Services[contracts.Parsing] = (*parser)(nil)
		return instance, err
	}
	if _, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory}); err == nil || len(closed) != 1 {
		t.Fatal("typed nil accepted or leaked")
	}
}

func TestLateCreationAfterCancellationIsEventuallyClosed(t *testing.T) {
	closed := make(chan struct{}, 1)
	release := make(chan struct{})
	started := make(chan struct{})
	factory := kernel.Factory{Manifest: kernel.Manifest{ID: "parser", Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.Parsing}},
		Validate: func(json.RawMessage) error { return nil },
		Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
			close(started)
			<-release
			return &kernel.Instance{Services: map[contracts.Capability]any{contracts.Parsing: parser{}}, Health: func(context.Context) error { return nil },
				Close: func(context.Context) error { closed <- struct{}{}; return nil }}, nil
		}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := kernel.Build(ctx, configuration("parser"), []kernel.Factory{factory}); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("creation did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled build succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled build did not return")
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("late instance was not reclaimed")
	}
}

func TestProviderReleaseMustBePinnedWhenCatalogueIsAmbiguous(t *testing.T) {
	var closed []string
	first, second := parserFactory("parser", &closed), parserFactory("parser", &closed)
	second.Manifest.Version = "1.1.0"
	config := configuration("parser")
	if _, err := kernel.Build(context.Background(), config, []kernel.Factory{first, second}); err == nil {
		t.Fatal("ambiguous release silently selected")
	}
	config.Modules[0].ImplementationVersion = "1.1.0"
	runtime, err := kernel.Build(context.Background(), config, []kernel.Factory{first, second})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	if runtime.Snapshot().Modules[0].ImplementationVersion != "1.1.0" {
		t.Fatal("wrong pinned release")
	}
}
