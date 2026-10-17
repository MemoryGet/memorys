package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/profileassembly"
)

var profileAssemblySamples []map[string]any

type managedNativeReader struct {
	mu     sync.RWMutex
	closed bool
	inner  postgres.KeywordRetriever
}

func (p *managedNativeReader) Search(c context.Context, r contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return nil, errors.New("native reader closed")
	}
	return p.inner.Search(c, r)
}
func assembleNativeReader(ctx context.Context, db *sql.DB, s configprofile.Snapshot, reader postgres.KeywordRetriever, rerankers ...contracts.Reranker) (*profileassembly.Runtime, contracts.Retriever, error) {
	factory := kernel.Factory{Manifest: kernel.Manifest{ID: "native-keyword-provider", Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.Retrieval}}, Validate: func(raw json.RawMessage) error {
		if string(raw) != "{}" {
			return errors.New("settings unsupported")
		}
		return nil
	}, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
		p := &managedNativeReader{inner: reader}
		return &kernel.Instance{Services: map[contracts.Capability]any{contracts.Retrieval: p}, Health: db.PingContext, Close: func(context.Context) error { p.mu.Lock(); defer p.mu.Unlock(); p.closed = true; return nil }}, nil
	}}
	plan := kernel.Config{Version: 1, Bindings: map[contracts.Capability]string{contracts.Retrieval: "postgres-keyword-reader"}, Modules: []kernel.ModuleConfig{{ID: "postgres-keyword-reader", Provider: factory.Manifest.ID, ImplementationVersion: "1.0.0", Settings: json.RawMessage(`{}`)}}}
	factories := []kernel.Factory{factory}
	if s.Query.Policy.EnableRerank {
		if len(rerankers) != 1 || rerankers[0] == nil {
			return nil, nil, errors.New("reranker unavailable")
		}
		f := kernel.Factory{Manifest: kernel.Manifest{ID: "native-test-reranker", Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.Reranking}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
			return &kernel.Instance{Services: map[contracts.Capability]any{contracts.Reranking: rerankers[0]}, Health: func(c context.Context) error { return c.Err() }, Close: func(context.Context) error { return nil }}, nil
		}}
		factories = append(factories, f)
		plan.Modules = append(plan.Modules, kernel.ModuleConfig{ID: string(s.Query.Bindings[contracts.Reranking].InstanceID), Provider: f.Manifest.ID, Settings: json.RawMessage(`{}`)})
		plan.Bindings[contracts.Reranking] = string(s.Query.Bindings[contracts.Reranking].InstanceID)
	}
	runtime, err := profileassembly.Build(ctx, s, profileassembly.Query, plan, factories)
	if err != nil {
		return nil, nil, err
	}
	service, err := profileassembly.ResolveRoute[contracts.Retriever](runtime, "keyword-route")
	if err != nil {
		_ = runtime.Close(context.Background())
		return nil, nil, err
	}
	return runtime, service, nil
}
