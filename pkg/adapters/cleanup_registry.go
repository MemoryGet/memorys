package adapters

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

type CleanupRegistryConfig struct {
	Source         contracts.CleanupModuleSource
	Scope          func(context.Context) (contracts.AccessScope, error)
	AuthorizeIndex func(context.Context, contracts.CleanupOperation) (contracts.IndexDeleteRequest, error)
	// Host-built runtimes only; deployment records select these keys, never load
	// code or create connections. Keep retired configurations until cleanup ends.
	Runtimes map[contracts.Digest]*kernel.Runtime
}
type CleanupRegistry struct{ config CleanupRegistryConfig }

func NewCleanupRegistry(c CleanupRegistryConfig) (*CleanupRegistry, error) {
	if missingPort(c.Source) || c.Scope == nil || c.AuthorizeIndex == nil || len(c.Runtimes) == 0 || len(c.Runtimes) > 1000 {
		return nil, ErrModule
	}
	owned := map[contracts.Digest]*kernel.Runtime{}
	for key, r := range c.Runtimes {
		if r == nil || len(key) != 64 || r.Snapshot().ConfigVersion != string(key) {
			return nil, ErrModule
		}
		owned[key] = r
	}
	c.Runtimes = owned
	return &CleanupRegistry{config: c}, nil
}
func registryCopy[T any](v T) (T, error) {
	var out T
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > 1<<20 {
		return out, ErrModule
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, ErrModule
	}
	return out, nil
}

func (r *CleanupRegistry) binding(ctx context.Context, op contracts.CleanupOperation) (contracts.CleanupModuleBinding, IndexCleanup, error) {
	var empty contracts.CleanupModuleBinding
	var none IndexCleanup
	if ctx == nil || ctx.Err() != nil || r == nil || missingPort(r.config.Source) || r.config.Scope == nil || r.config.AuthorizeIndex == nil || op.Target.Kind != "index" || op.Target.GenerationID == nil || op.Target.BackendEpochID == nil || op.Target.Channel == nil || *op.Target.Channel != contracts.KeywordChannel {
		return empty, none, ErrModule
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, none, ErrModule
	}
	op, e := registryCopy(op)
	if e != nil {
		return empty, none, e
	}
	expected, e := cleanup.Operation(contracts.CleanupLease{SpaceID: op.SpaceID, KnowledgeBaseID: op.KnowledgeBaseID, DocumentID: op.DocumentID, PlanID: op.PlanID, TargetID: op.TargetID, RevisionID: op.RevisionID, GenerationSetID: op.GenerationSetID, PublicationEpoch: op.PublicationEpoch, Target: op.Target})
	if e != nil || !reflect.DeepEqual(expected, op) {
		return empty, none, ErrModule
	}
	scope, e := r.config.Scope(ctx)
	if e != nil {
		return empty, none, ErrModule
	}
	scope, e = registryCopy(scope)
	if e != nil {
		return empty, none, e
	}
	if scope.Action != "publication.cleanup.execute" || scope.SubjectID == "" || scope.ScopeID == "" || scope.SpaceID != op.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != op.KnowledgeBaseID || scope.PolicyVersion == 0 || !scope.ExpiresAt.After(time.Now()) {
		return empty, none, ErrModule
	}
	argument, e := registryCopy(op)
	if e != nil {
		return empty, none, e
	}
	b, e := r.config.Source.ResolveCleanupModule(ctx, scope, argument)
	if e != nil {
		return empty, none, ErrModule
	}
	b, e = registryCopy(b)
	if e != nil || ctx.Err() != nil || b.Version == 0 || b.SpaceID != op.SpaceID || b.KnowledgeBaseID != op.KnowledgeBaseID || b.BindingReference != op.Target.BindingReference || b.ModuleInstanceID != op.Target.ModuleInstanceID || b.ProfileDigest != op.Target.ProfileDigest || b.GenerationID != *op.Target.GenerationID || b.BackendEpochID != *op.Target.BackendEpochID || b.Channel != *op.Target.Channel || b.ContractVersion == "" || b.Provider == "" || b.ImplementationVersion == "" {
		return empty, none, ErrModule
	}
	runtime := r.config.Runtimes[b.RuntimeConfigDigest]
	if runtime == nil {
		return empty, none, ErrModule
	}
	snapshot := runtime.Snapshot()
	if snapshot.ConfigVersion != string(b.RuntimeConfigDigest) {
		return empty, none, ErrModule
	}
	matched := false
	for _, module := range snapshot.Modules {
		if module.ID == string(b.ModuleInstanceID) && module.Provider == b.Provider && module.ImplementationVersion == b.ImplementationVersion {
			matched = true
			break
		}
	}
	if !matched {
		return empty, none, ErrModule
	}
	value, e := runtime.GetBound(contracts.ModuleBinding{InstanceID: b.ModuleInstanceID, Capability: contracts.IndexCleanupCapability, ContractVersion: b.ContractVersion, RequiredFeatures: []string{"generation.delete", "generation.inspect", "delete.fence", "fence.durable"}})
	if e != nil {
		return empty, none, ErrModule
	}
	port, ok := value.(contracts.IndexCleanupBackend)
	if !ok || missingPort(port) {
		return empty, none, ErrModule
	}
	return b, IndexCleanup{Index: port, ModuleInstanceID: b.ModuleInstanceID, ProfileDigest: b.ProfileDigest, Authorize: r.config.AuthorizeIndex}, nil
}
func (r *CleanupRegistry) Resolve(ctx context.Context, op contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
	owned, e := registryCopy(op)
	if e != nil {
		return nil, e
	}
	b, _, e := r.binding(ctx, owned)
	if e != nil {
		return nil, e
	}
	return &registeredCleanup{registry: r, operation: owned, binding: b}, nil
}

// VerifyReceipt uses the same pinned deployment identity but rereads backend
// facts through the actual registered port, rather than accepting echo fields.
func (r *CleanupRegistry) VerifyReceipt(ctx context.Context, op contracts.CleanupOperation, receipt contracts.CleanupReceipt) error {
	_, adapter, e := r.binding(ctx, op)
	if e != nil {
		return e
	}
	return adapter.VerifyReceipt(ctx, op, receipt)
}

type registeredCleanup struct {
	registry  *CleanupRegistry
	operation contracts.CleanupOperation
	binding   contracts.CleanupModuleBinding
}

func (p *registeredCleanup) adapter(ctx context.Context, op contracts.CleanupOperation) (IndexCleanup, error) {
	if !reflect.DeepEqual(op, p.operation) {
		return IndexCleanup{}, ErrModule
	}
	b, a, e := p.registry.binding(ctx, op)
	if e != nil || !reflect.DeepEqual(b, p.binding) {
		return IndexCleanup{}, ErrModule
	}
	return a, nil
}
func (p *registeredCleanup) Lookup(ctx context.Context, op contracts.CleanupOperation) (contracts.CleanupReceipt, error) {
	a, e := p.adapter(ctx, op)
	if e != nil {
		return contracts.CleanupReceipt{}, e
	}
	return a.Lookup(ctx, op)
}
func (p *registeredCleanup) Execute(ctx context.Context, op contracts.CleanupOperation) (contracts.CleanupReceipt, error) {
	a, e := p.adapter(ctx, op)
	if e != nil {
		return contracts.CleanupReceipt{}, e
	}
	return a.Execute(ctx, op)
}
