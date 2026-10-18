package knowledge

import (
	"context"
	"sync/atomic"

	"github.com/MemoryGet/memorys/pkg/clockhealth"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// ConfiguredReadRuntime owns the current/history read application and its
// lifecycle. Host bootstrap owns the supplied stores, credentials and modules.
type ConfiguredReadRuntime struct {
	service *CurrentReadService
	closed  atomic.Bool
}
type ConfiguredReadRuntimeConfig struct {
	Dependencies CurrentReadDependencies
	ClockHealth  func(context.Context) error
}

func NewConfiguredReadRuntime(c ConfiguredReadRuntimeConfig) (*ConfiguredReadRuntime, error) {
	if clockhealth.Check(context.Background(), c.ClockHealth) != nil {
		return nil, ErrRead
	}
	service, e := NewCurrentRead(c.Dependencies)
	if e != nil {
		return nil, e
	}
	return &ConfiguredReadRuntime{service: service}, nil
}
func (r *ConfiguredReadRuntime) ReadCurrent(ctx context.Context, in CurrentReadInput) (contracts.FrozenContentRecord, error) {
	if r == nil || r.closed.Load() {
		return contracts.FrozenContentRecord{}, ErrRead
	}
	return r.service.ReadCurrent(ctx, in)
}
func (r *ConfiguredReadRuntime) Close() {
	if r != nil {
		r.closed.Store(true)
	}
}
