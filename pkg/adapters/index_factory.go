package adapters

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

// IndexStoreFactory assembles host-owned, preconfigured reference stores. It is
// not a deployment loader: credentials/DB/authority are supplied by the host.
// Only the narrow local ports are registered, never IndexWriter or metadata_uow.
func IndexStoreFactory(provider string, backend contracts.IndexCleanupBackend, snapshots contracts.IndexSnapshotStore, health func(context.Context) error, durable bool) kernel.Factory {
	validate := func(raw json.RawMessage) error {
		encoded, err := canonical.JSON(raw)
		if err != nil || string(encoded) != "{}" || missingPort(backend) || missingPort(snapshots) || health == nil {
			return ErrModule
		}
		return nil
	}
	cleanupFeatures := []string{"generation.delete", "generation.inspect", "delete.fence"}
	if durable {
		cleanupFeatures = append(cleanupFeatures, "fence.durable")
	}
	return kernel.Factory{Manifest: kernel.Manifest{ID: provider, Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.IndexCleanupCapability, contracts.IndexSnapshotCapability}, CapabilityVersions: map[contracts.Capability]string{contracts.IndexCleanupCapability: "1.0", contracts.IndexSnapshotCapability: "1.0"}, CapabilityFeatures: map[contracts.Capability][]string{contracts.IndexCleanupCapability: cleanupFeatures, contracts.IndexSnapshotCapability: {"snapshot.export", "snapshot.import", "snapshot.delete_fences", "target.empty"}}}, Validate: validate, Create: func(ctx context.Context, raw json.RawMessage, _ kernel.Lookup) (*kernel.Instance, error) {
		if ctx == nil || ctx.Err() != nil || validate(raw) != nil {
			return nil, ErrModule
		}
		if err := health(ctx); err != nil {
			return nil, err
		}
		service := &managedIndex{backend: backend, snapshots: snapshots}
		return &kernel.Instance{Services: map[contracts.Capability]any{contracts.IndexCleanupCapability: service, contracts.IndexSnapshotCapability: service}, Health: func(c context.Context) error {
			if err := service.check(c); err != nil {
				return err
			}
			return health(c)
		}, Close: func(context.Context) error { service.closed.Store(true); return nil }}, nil
	}}
}
func missingPort(port any) bool {
	if port == nil {
		return true
	}
	v := reflect.ValueOf(port)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// The host drains in-flight work before Close and owns the shared backend/DB.
// Closing this facade invalidates escaped references without closing the DB.
type managedIndex struct {
	backend   contracts.IndexCleanupBackend
	snapshots contracts.IndexSnapshotStore
	closed    atomic.Bool
}

func (m *managedIndex) check(ctx context.Context) error {
	if ctx == nil || m.closed.Load() {
		return ErrModule
	}
	return ctx.Err()
}
func (m *managedIndex) Delete(ctx context.Context, req contracts.IndexDeleteRequest) error {
	if err := m.check(ctx); err != nil {
		return err
	}
	return m.backend.Delete(ctx, req)
}
func (m *managedIndex) InspectGeneration(ctx context.Context, scope contracts.AccessScope, id contracts.ID) (contracts.IndexDeletionState, error) {
	if err := m.check(ctx); err != nil {
		return contracts.IndexDeletionState{}, err
	}
	return m.backend.InspectGeneration(ctx, scope, id)
}
func (m *managedIndex) ExportIndex(ctx context.Context, scope contracts.AccessScope, ids []contracts.ID) (contracts.IndexSnapshot, error) {
	if err := m.check(ctx); err != nil {
		return contracts.IndexSnapshot{}, err
	}
	return m.snapshots.ExportIndex(ctx, scope, ids)
}
func (m *managedIndex) ImportIndex(ctx context.Context, scope contracts.AccessScope, snapshot contracts.IndexSnapshot) error {
	if err := m.check(ctx); err != nil {
		return err
	}
	return m.snapshots.ImportIndex(ctx, scope, snapshot)
}
