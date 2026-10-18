package kernel

import (
	"context"
	"errors"
	"sync"
)

var ErrFactoryRegistry = errors.New("module factory registry rejected")

// FactoryRegistry is an immutable provider catalogue for one deployment
// generation. Provider identity and implementation release are selected from
// the sealed module config; callers cannot register factories after assembly.
type FactoryRegistry struct {
	mu        sync.RWMutex
	factories []Factory
}

func NewFactoryRegistry(factories []Factory) (*FactoryRegistry, error) {
	if len(factories) == 0 || len(factories) > 256 {
		return nil, ErrFactoryRegistry
	}
	seen := map[string]bool{}
	owned := make([]Factory, 0, len(factories))
	for _, factory := range factories {
		if err := validateManifest(factory.Manifest); err != nil || factory.Validate == nil || factory.Create == nil {
			return nil, ErrFactoryRegistry
		}
		key := factory.Manifest.ID + "\x00" + factory.Manifest.Version
		if seen[key] {
			return nil, ErrFactoryRegistry
		}
		seen[key] = true
		factory.Manifest = cloneManifest(factory.Manifest)
		owned = append(owned, factory)
	}
	return &FactoryRegistry{factories: owned}, nil
}

func (r *FactoryRegistry) Resolve(ctx context.Context, module ModuleConfig) (Factory, error) {
	if r == nil || ctx == nil || ctx.Err() != nil {
		return Factory{}, ErrFactoryRegistry
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, err := selectFactory(module, r.factories)
	if err != nil {
		return Factory{}, ErrFactoryRegistry
	}
	return factory, nil
}
