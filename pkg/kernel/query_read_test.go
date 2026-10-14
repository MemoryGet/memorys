package kernel_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type readRepository struct{ identity contracts.QueryReadIdentity }

func (r readRepository) ReadPublishedPage(context.Context, contracts.PublishedPageRequest) (contracts.PublishedPage, error) {
	return contracts.PublishedPage{Identity: r.identity}, nil
}
func (readRepository) LoadActiveEpoch(context.Context, contracts.ID) (contracts.BackendEpoch, error) {
	return contracts.BackendEpoch{}, errors.New("unused")
}
func (readRepository) LoadCurrentDocument(context.Context, contracts.ID) (contracts.CurrentDocumentState, error) {
	return contracts.CurrentDocumentState{}, errors.New("unused")
}

type readUnit struct {
	identity contracts.QueryReadIdentity
	closed   *atomic.Bool
}

func (u readUnit) WithinRead(c context.Context, _ contracts.AccessScope, cb func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	if u.closed.Load() {
		return queryread.ErrClosed
	}
	return cb(c, u.identity, readRepository{u.identity})
}
func readFactory(id string) kernel.Factory {
	return kernel.Factory{Manifest: kernel.Manifest{ID: id, Version: "1.0.0", ContractVersion: "1.0.0", Capabilities: []contracts.Capability{contracts.QueryReading}, CapabilityFeatures: map[contracts.Capability][]string{contracts.QueryReading: {"snapshot.fixed_view", "lifecycle.scoped_read"}}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
		closed := &atomic.Bool{}
		unit, err := queryread.ProtectUnit(readUnit{contracts.QueryReadIdentity{StoreEpochID: "store", ReadVersion: id, Watermark: "watermark"}, closed})
		if err != nil {
			return nil, err
		}
		return &kernel.Instance{Services: map[contracts.Capability]any{contracts.QueryReading: unit}, Health: func(context.Context) error { return nil }, Close: func(context.Context) error { closed.Store(true); return nil }}, nil
	}}
}
func readConfig(provider string) kernel.Config {
	return kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "reader", Provider: provider, Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.QueryReading: "reader"}}
}

func TestReadUnitProviderReplacementPreservesGuardedPort(t *testing.T) {
	for _, provider := range []string{"reader.first", "reader.second"} {
		runtime, e := kernel.Build(context.Background(), readConfig(provider), []kernel.Factory{readFactory(provider)}, kernel.Requirement{Capability: contracts.QueryReading, Features: []string{"snapshot.fixed_view", "lifecycle.scoped_read"}})
		if e != nil {
			t.Fatal(e)
		}
		unit, e := kernel.Resolve[contracts.QueryReadUnit](runtime, contracts.QueryReading)
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		scope := contracts.AccessScope{ScopeID: "scope", SubjectID: "user", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "knowledge.search", PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}
		var escaped contracts.QueryReadRepository
		e = unit.WithinRead(ctx, scope, func(c context.Context, id contracts.QueryReadIdentity, r contracts.QueryReadRepository) error {
			if id.ReadVersion != provider {
				t.Fatal("wrong implementation")
			}
			escaped = r
			_, err := r.ReadPublishedPage(c, contracts.PublishedPageRequest{Identity: id, KnowledgeBaseIDs: []contracts.ID{"kb"}, Limit: 1})
			return err
		})
		if e != nil {
			t.Fatal(e)
		}
		if _, e := escaped.LoadCurrentDocument(ctx, "doc"); e != queryread.ErrClosed {
			t.Fatal("callback handle remained live", e)
		}
		if e := runtime.Close(ctx); e != nil {
			t.Fatal(e)
		}
		if e := unit.WithinRead(ctx, scope, func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error {
			t.Fatal("closed provider ran callback")
			return nil
		}); e == nil {
			t.Fatal("closed retained service accepted read")
		}
		cancel()
	}
}

func TestReadUnitFeaturesAndPortAreEnforced(t *testing.T) {
	f := readFactory("reader")
	delete(f.Manifest.CapabilityFeatures, contracts.QueryReading)
	if _, e := kernel.Build(context.Background(), readConfig("reader"), []kernel.Factory{f}, kernel.Requirement{Capability: contracts.QueryReading, Features: []string{"snapshot.fixed_view"}}); e == nil {
		t.Fatal("missing fixed-view feature accepted")
	}
	f = readFactory("reader")
	create := f.Create
	f.Create = func(c context.Context, s json.RawMessage, l kernel.Lookup) (*kernel.Instance, error) {
		i, e := create(c, s, l)
		i.Services[contracts.QueryReading] = "wrong interface"
		return i, e
	}
	if _, e := kernel.Build(context.Background(), readConfig("reader"), []kernel.Factory{f}); e == nil {
		t.Fatal("wrong read port accepted")
	}
}
