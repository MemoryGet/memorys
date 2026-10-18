package adapters

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/index"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

type cleanupModuleSource func(context.Context, contracts.AccessScope, contracts.CleanupOperation) (contracts.CleanupModuleBinding, error)

func (f cleanupModuleSource) ResolveCleanupModule(c context.Context, s contracts.AccessScope, op contracts.CleanupOperation) (contracts.CleanupModuleBinding, error) {
	return f(c, s, op)
}

func TestCleanupRegistryPinsNamedRuntimeAndRechecksEscapedExecutor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, op, _ := memoryCleanupFixture(t)
	m := a.Index.(*index.MemoryIndex)
	// Unit-only feature declaration; this does not prove memory durability.
	factory := IndexStoreFactory("fixture.index", m, m, func(context.Context) error { return nil }, true)
	decoy := &index.MemoryIndex{}
	wrongDefault := IndexStoreFactory("fixture.decoy", decoy, decoy, func(context.Context) error { return nil }, true)
	plan := kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "engine", Provider: "fixture.index", Settings: json.RawMessage(`{}`)}, {ID: "decoy", Provider: "fixture.decoy", Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.IndexCleanupCapability: "decoy"}}
	runtime, e := kernel.Build(ctx, plan, []kernel.Factory{factory, wrongDefault})
	if e != nil {
		t.Fatal(e)
	}
	defer runtime.Close(ctx)
	digest := contracts.Digest(runtime.Snapshot().ConfigVersion)
	binding := contracts.CleanupModuleBinding{Version: 1, SpaceID: "space", KnowledgeBaseID: "kb", BindingReference: "binding", ModuleInstanceID: "engine", ProfileDigest: op.Target.ProfileDigest, RuntimeConfigDigest: digest, Provider: "fixture.index", ImplementationVersion: "1.0.0", ContractVersion: "1.0", GenerationID: "generation", BackendEpochID: "epoch", Channel: contracts.KeywordChannel}
	mutate := false
	source := cleanupModuleSource(func(_ context.Context, _ contracts.AccessScope, v contracts.CleanupOperation) (contracts.CleanupModuleBinding, error) {
		b := binding
		if mutate {
			*v.Target.GenerationID = "foreign"
			b.GenerationID = "foreign"
		}
		return b, nil
	})
	runtimes := map[contracts.Digest]*kernel.Runtime{digest: runtime}
	registry, e := NewCleanupRegistry(CleanupRegistryConfig{Source: source, Scope: func(c context.Context) (contracts.AccessScope, error) {
		d, _ := c.Deadline()
		return contracts.AccessScope{ScopeID: "scope", SubjectID: "worker", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "publication.cleanup.execute", PolicyVersion: 1, ExpiresAt: d}, nil
	}, AuthorizeIndex: a.Authorize, Runtimes: runtimes})
	if e != nil {
		t.Fatal(e)
	}
	delete(runtimes, digest)
	executor, e := registry.Resolve(ctx, op)
	if e != nil {
		t.Fatal("named module/config map not owned", e)
	}
	if receipt, e := executor.Lookup(ctx, op); e != nil || receipt.State != "absent" {
		t.Fatal(e, receipt)
	}
	binding.Version = 2
	if _, e = executor.Execute(ctx, op); e == nil {
		t.Fatal("escaped executor ignored deployment version change")
	}
	binding.Version = 1
	for name, change := range map[string]func(){"provider": func() { binding.Provider = "other" }, "implementation": func() { binding.ImplementationVersion = "2.0.0" }, "profile": func() { binding.ProfileDigest = contracts.Digest(strings.Repeat("b", 64)) }, "runtime": func() { binding.RuntimeConfigDigest = contracts.Digest(strings.Repeat("f", 64)) }, "contract": func() { binding.ContractVersion = "2.0" }} {
		saved := binding
		change()
		if _, e = registry.Resolve(ctx, op); e == nil {
			t.Fatal("mismatch admitted", name)
		}
		binding = saved
	}
	mutate = true
	if _, e = registry.Resolve(ctx, op); e == nil {
		t.Fatal("source mutated fixed operation")
	}
	mutate = false
	if *op.Target.GenerationID != "generation" {
		t.Fatal("caller operation rewritten")
	}
	receipt, e := executor.Execute(ctx, op)
	if e != nil || receipt.State != "succeeded" || registry.VerifyReceipt(ctx, op, receipt) != nil {
		t.Fatal("registered backend actual effect failed", e)
	}
	if e = runtime.Close(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = executor.Lookup(ctx, op); e == nil {
		t.Fatal("escaped executor survived runtime close")
	}
}

func TestCleanupRegistryRejectsUnprovenDurability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a, op, _ := memoryCleanupFixture(t)
	m := a.Index.(*index.MemoryIndex)
	f := IndexStoreFactory("fixture.memory", m, m, func(context.Context) error { return nil }, false)
	r, e := kernel.Build(ctx, kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "engine", Provider: f.Manifest.ID, Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.IndexCleanupCapability: "engine"}}, []kernel.Factory{f})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close(ctx)
	digest := contracts.Digest(r.Snapshot().ConfigVersion)
	registry, e := NewCleanupRegistry(CleanupRegistryConfig{Runtimes: map[contracts.Digest]*kernel.Runtime{digest: r}, AuthorizeIndex: a.Authorize, Scope: func(context.Context) (contracts.AccessScope, error) {
		return contracts.AccessScope{ScopeID: "scope", SubjectID: "worker", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "publication.cleanup.execute", PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}, nil
	}, Source: cleanupModuleSource(func(context.Context, contracts.AccessScope, contracts.CleanupOperation) (contracts.CleanupModuleBinding, error) {
		return contracts.CleanupModuleBinding{Version: 1, SpaceID: op.SpaceID, KnowledgeBaseID: op.KnowledgeBaseID, BindingReference: op.Target.BindingReference, ModuleInstanceID: op.Target.ModuleInstanceID, ProfileDigest: op.Target.ProfileDigest, RuntimeConfigDigest: digest, Provider: f.Manifest.ID, ImplementationVersion: "1.0.0", ContractVersion: "1.0", GenerationID: *op.Target.GenerationID, BackendEpochID: *op.Target.BackendEpochID, Channel: *op.Target.Channel}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = registry.Resolve(ctx, op); e == nil {
		t.Fatal("missing durable fence feature admitted")
	}
}
