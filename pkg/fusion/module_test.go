package fusion_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/fusion"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

func fusionConfig(raw json.RawMessage) kernel.Config {
	return kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "fuser", Provider: "platform.rrf", Settings: raw}}, Bindings: map[contracts.Capability]string{contracts.Fusion: "fuser"}}
}

func TestRRFModuleAssemblyAndRestartBinding(t *testing.T) {
	p, q := profiles()
	var previous string
	for i := 0; i < 2; i++ {
		if i == 1 {
			p.Version++
			p.Digest = contracts.Digest(strings.Repeat("c", 64))
		}
		factory, settings, err := fusion.NewRRFModule(p, q)
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := kernel.Build(context.Background(), fusionConfig(settings), []kernel.Factory{factory}, kernel.Requirement{Capability: contracts.Fusion})
		if err != nil {
			t.Fatal(err)
		}
		f, err := kernel.Resolve[contracts.Fuser](runtime, contracts.Fusion)
		if err != nil {
			t.Fatal(err)
		}
		r := configuredRequest(routeA())
		r.Request.ProfileDigest = p.Digest
		if out, err := f.Fuse(context.Background(), r); err != nil || len(out) != 1 {
			t.Fatal(out, err)
		}
		current := runtime.Snapshot().ConfigVersion
		if current == previous {
			t.Fatal("changed profile missing from assembly digest")
		}
		previous = current
		if err := runtime.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Fuse(context.Background(), r); err == nil {
			t.Fatal("retained reference worked after close")
		}
		if _, err := runtime.Get(contracts.Fusion); err == nil {
			t.Fatal("closed runtime resolved service")
		}
	}
}

func TestRRFModuleRejectsWrongAndMalformedSettings(t *testing.T) {
	p, q := profiles()
	factory, settings, err := fusion.NewRRFModule(p, q)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := json.Unmarshal(settings, &values); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `null`, `[]`, `{"profile_identity":"wrong"}`, `{"PROFILE_IDENTITY":"` + values["profile_identity"] + `"}`, string(settings) + `{}`, `{"profile_identity":"` + values["profile_identity"] + `","profile_identity":"` + values["profile_identity"] + `"}`} {
		if _, err := kernel.Build(context.Background(), fusionConfig(json.RawMessage(raw)), []kernel.Factory{factory}); err == nil {
			t.Fatal("accepted invalid settings", raw)
		}
		if _, err := factory.Create(context.Background(), json.RawMessage(raw), nil); err == nil {
			t.Fatal("direct creation skipped validation")
		}
	}
	p.Version++
	other, _, err := fusion.NewRRFModule(p, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kernel.Build(context.Background(), fusionConfig(settings), []kernel.Factory{other}); err == nil {
		t.Fatal("accepted settings from another profile")
	}
}

func TestRRFModuleInstancesHaveIndependentLifecycles(t *testing.T) {
	p, q := profiles()
	factory, settings, err := fusion.NewRRFModule(p, q)
	if err != nil {
		t.Fatal(err)
	}
	a, err := factory.Create(context.Background(), settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := factory.Create(context.Background(), settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close(context.Background())
	if err := a.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Health(context.Background()); err == nil {
		t.Fatal("closed instance is healthy")
	}
	if _, err := b.Services[contracts.Fusion].(contracts.Fuser).Fuse(context.Background(), configuredRequest(routeA())); err != nil {
		t.Fatal("closing one instance stopped another", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := factory.Create(ctx, settings, nil); err == nil {
		t.Fatal("cancelled creation succeeded")
	}
}
