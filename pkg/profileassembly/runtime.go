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
