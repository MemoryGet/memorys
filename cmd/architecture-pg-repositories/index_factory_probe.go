package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/index"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

func verifyConfiguredIndexStores(ctx context.Context, db *sql.DB, scope contracts.AccessScope, ids []contracts.ID, m *index.MemoryIndex, pg postgres.IndexCleanup, expected contracts.Digest) error {
	factories := []kernel.Factory{adapters.IndexStoreFactory("reference.index.memory", m, m, func(c context.Context) error { return c.Err() }, false), adapters.IndexStoreFactory("reference.index.postgres", pg, pg, db.PingContext, true)}
	var firstVersion string
	for _, provider := range []string{"reference.index.memory", "reference.index.postgres"} {
		plan := kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "index-store", Provider: provider, Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.IndexCleanupCapability: "index-store", contracts.IndexSnapshotCapability: "index-store"}}
		requirements := []kernel.Requirement{{Capability: contracts.IndexSnapshotCapability, Features: []string{"snapshot.export", "snapshot.import", "snapshot.delete_fences"}}}
		if provider == "reference.index.postgres" {
			requirements = append(requirements, kernel.Requirement{Capability: contracts.IndexCleanupCapability, Features: []string{"fence.durable"}})
		}
		runtime, err := kernel.Build(ctx, plan, factories, requirements...)
		if err != nil {
			return err
		}
		port, err := kernel.Resolve[contracts.IndexSnapshotStore](runtime, contracts.IndexSnapshotCapability)
		if err != nil {
			runtime.Close(ctx)
			return err
		}
		snapshot, err := port.ExportIndex(ctx, scope, ids)
		if err != nil || snapshot.Checksum != expected {
			runtime.Close(ctx)
			return errors.New("configured index changed snapshot")
		}
		version := runtime.Snapshot().ConfigVersion
		if firstVersion == "" {
			firstVersion = version
		} else if firstVersion == version {
			runtime.Close(ctx)
			return errors.New("provider change did not bind config version")
		}
		if err := runtime.Close(ctx); err != nil {
			return err
		}
		if _, err := port.ExportIndex(ctx, scope, ids); err == nil {
			return errors.New("closed configured index reference usable")
		}
	}
	// Host-owned backend and shared DB survive facade shutdown.
	if _, err := pg.ExportIndex(ctx, scope, ids); err != nil {
		return err
	}
	return nil
}
