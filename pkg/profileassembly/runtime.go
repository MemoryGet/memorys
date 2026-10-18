// Package profileassembly binds verified platform profiles to the module kernel.
package profileassembly

import (
	"context"
	"errors"
	"sort"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

type Stage string

const (
	Query Stage = "query"
	Index Stage = "index"
)

var ErrAssembly = errors.New("profile assembly rejected")

type Runtime struct {
	inner   *kernel.Runtime
	payload []byte
	stage   Stage
}

// FactoryResolver is the host-owned provider/catalog boundary. It receives a
// copied module declaration and must resolve it from a trusted factory
// registry; request fields never choose a provider or settings here.
type FactoryResolver func(context.Context, kernel.ModuleConfig) (kernel.Factory, error)

// BuildFromFiles loads one sealed profile and one strict module configuration,
// resolves each named module through the host registry, then applies the same
// stage binding and kernel health checks as Build. It does not open databases
// or fetch remote modules.
func BuildFromFiles(ctx context.Context, profilePath, modulePath string, stage Stage, resolve FactoryResolver) (*Runtime, error) {
	if ctx == nil || resolve == nil || profilePath == "" || modulePath == "" {
		return nil, ErrAssembly
	}
	profile, err := configprofile.LoadFile(profilePath)
	if err != nil {
		return nil, ErrAssembly
	}
	configuration, err := kernel.LoadConfigFile(modulePath)
	if err != nil || len(configuration.Modules) == 0 {
		return nil, ErrAssembly
	}
	factories := make([]kernel.Factory, 0, len(configuration.Modules))
	for _, module := range configuration.Modules {
		factory, err := resolve(ctx, module)
		if err != nil || factory.Manifest.ID == "" || factory.Manifest.ID != module.Provider && factory.Manifest.ID != module.ID {
			return nil, ErrAssembly
		}
		factories = append(factories, factory)
	}
	return Build(ctx, profile, stage, configuration, factories)
}

func BuildFromFilesWithRegistry(ctx context.Context, profilePath, modulePath string, stage Stage, registry *kernel.FactoryRegistry) (*Runtime, error) {
	if registry == nil {
		return nil, ErrAssembly
	}
	return BuildFromFiles(ctx, profilePath, modulePath, stage, registry.Resolve)
}

// Build checks every selected stage's named binding before constructing modules.
// Query-stage readiness never asserts that index writing is available.
func Build(ctx context.Context, s configprofile.Snapshot, stage Stage, plan kernel.Config, factories []kernel.Factory) (*Runtime, error) {
	raw, err := configprofile.Encode(s)
	if err != nil {
		return nil, ErrAssembly
	}
	owned, err := configprofile.Decode(raw)
	if err != nil {
		return nil, ErrAssembly
	}
	var selected map[contracts.Capability]contracts.ModuleBinding
	switch stage {
	case Query:
		selected = owned.Query.Bindings
	case Index:
		selected = owned.Index.Bindings
	default:
		return nil, ErrAssembly
	}
	keys := []string{}
	for cap := range selected {
		keys = append(keys, string(cap))
	}
	sort.Strings(keys)
	bindings := []contracts.ModuleBinding{}
	for _, cap := range keys {
		bindings = append(bindings, selected[contracts.Capability(cap)])
	}
	if stage == Index {
		for _, route := range owned.Index.Routes {
			bindings = append(bindings, route.Binding)
		}
	} else {
		for _, route := range owned.Query.Routes {
			bindings = append(bindings, route.Binding)
		}
	}
	runtime, err := kernel.BuildBound(ctx, plan, factories, bindings)
	if err != nil {
		return nil, ErrAssembly
	}
	return &Runtime{inner: runtime, payload: raw, stage: stage}, nil
}
func (r *Runtime) Profile() (configprofile.Snapshot, error) {
	if r == nil {
		return configprofile.Snapshot{}, ErrAssembly
	}
	return configprofile.Decode(r.payload)
}
func (r *Runtime) Identity() (kernel.Snapshot, error) {
	if r == nil || r.inner == nil {
		return kernel.Snapshot{}, ErrAssembly
	}
	return r.inner.Snapshot(), nil
}
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil || r.inner == nil {
		return ErrAssembly
	}
	return r.inner.Close(ctx)
}
func Resolve[T any](r *Runtime, cap contracts.Capability) (T, error) {
	var zero T
	if r == nil {
		return zero, ErrAssembly
	}
	s, err := r.Profile()
	if err != nil {
		return zero, err
	}
	selected := s.Query.Bindings
	if r.stage == Index {
		selected = s.Index.Bindings
	}
	b, ok := selected[cap]
	if !ok {
		return zero, ErrAssembly
	}
	value, err := r.inner.GetBound(b)
	if err != nil {
		return zero, ErrAssembly
	}
	typed, ok := value.(T)
	if !ok {
		return zero, ErrAssembly
	}
	return typed, nil
}

// ResolveRoute preserves each route's named implementation, including multiple
// retrievers offering the same capability.
func ResolveRoute[T any](r *Runtime, id contracts.ID) (T, error) {
	var zero T
	if r == nil {
		return zero, ErrAssembly
	}
	s, err := r.Profile()
	if err != nil {
		return zero, err
	}
	var binding contracts.ModuleBinding
	found := false
	if r.stage == Query {
		for _, route := range s.Query.Routes {
			if route.ID == id {
				binding = route.Binding
				found = true
			}
		}
	} else {
		for _, route := range s.Index.Routes {
			if route.ID == id {
				binding = route.Binding
				found = true
			}
		}
	}
	if !found {
		return zero, ErrAssembly
	}
	value, err := r.inner.GetBound(binding)
	if err != nil {
		return zero, ErrAssembly
	}
	result, ok := value.(T)
	if !ok {
		return zero, ErrAssembly
	}
	return result, nil
}
