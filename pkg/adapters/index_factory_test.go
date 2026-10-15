package adapters_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/index"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

func indexPlan(provider string) kernel.Config {
	return kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "index-store", Provider: provider, Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.IndexCleanupCapability: "index-store", contracts.IndexSnapshotCapability: "index-store"}}
}
func TestIndexFactoryRejectsUnprovenDurabilityAndClosedReferences(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m := &index.MemoryIndex{}
	f := adapters.IndexStoreFactory("reference.index.memory", m, m, func(context.Context) error { return nil }, false)
	if _, err := kernel.Build(ctx, indexPlan(f.Manifest.ID), []kernel.Factory{f}, kernel.Requirement{Capability: contracts.IndexCleanupCapability, Features: []string{"fence.durable"}}); err == nil {
		t.Fatal("memory durability requirement admitted")
	}
	runtime, err := kernel.Build(ctx, indexPlan(f.Manifest.ID), []kernel.Factory{f}, kernel.Requirement{Capability: contracts.IndexSnapshotCapability, Features: []string{"snapshot.delete_fences"}})
	if err != nil {
		t.Fatal(err)
	}
	port, err := kernel.Resolve[contracts.IndexSnapshotStore](runtime, contracts.IndexSnapshotCapability)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := port.ExportIndex(ctx, contracts.AccessScope{}, []contracts.ID{"id"}); err == nil {
		t.Fatal("escaped closed reference usable")
	}
}
func TestIndexFactoryRejectsTypedNilAndSettings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var missing *index.MemoryIndex
	f := adapters.IndexStoreFactory("reference.index.missing", missing, missing, func(context.Context) error { return nil }, false)
	if _, err := kernel.Build(ctx, indexPlan(f.Manifest.ID), []kernel.Factory{f}); err == nil {
		t.Fatal("typed nil backend admitted")
	}
	m := &index.MemoryIndex{}
	f = adapters.IndexStoreFactory("reference.index.memory", m, m, func(context.Context) error { return nil }, false)
	plan := indexPlan(f.Manifest.ID)
	plan.Modules[0].Settings = json.RawMessage(`{"database_password":"must-not-be-settings"}`)
	if _, err := kernel.Build(ctx, plan, []kernel.Factory{f}); err == nil {
		t.Fatal("unexpected settings admitted")
	}
}
