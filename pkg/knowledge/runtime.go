package knowledge

import (
	"context"
	"reflect"
	"sync/atomic"

	"github.com/MemoryGet/memorys/pkg/clockhealth"
	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// ConfiguredRuntime owns the application planner and search service as one
// closeable unit. Host bootstrap still owns database/auth/module lifetimes.
type ConfiguredRuntime struct {
	plans   *ConfiguredPlans
	service *SearchService
	closed  atomic.Bool
}
type ConfiguredRuntimeConfig struct {
	Profile      configprofile.Snapshot
	Dependencies Dependencies
	Limits       queryread.Limits
	Resolve      func(context.Context, contracts.ModuleBinding) (contracts.Retriever, error)
	Reranker     contracts.Reranker
	ClockHealth  func(context.Context) error
}

func NewConfiguredRuntime(c ConfiguredRuntimeConfig) (*ConfiguredRuntime, error) {
	if c.Dependencies.Auth == nil || c.Dependencies.Authorizer == nil || c.Dependencies.Facts == nil || c.Dependencies.Reads == nil || c.Dependencies.Backend == nil || clockhealth.Check(context.Background(), c.ClockHealth) != nil {
		return nil, ErrSearch
	}
	if reflect.ValueOf(c.Dependencies.Auth).Kind() == reflect.Pointer && reflect.ValueOf(c.Dependencies.Auth).IsNil() {
		return nil, ErrSearch
	}
	plans, e := NewConfiguredPlans(ConfiguredPlansConfig{Profile: c.Profile, Authorizer: c.Dependencies.Authorizer, Facts: c.Dependencies.Facts, Reads: c.Dependencies.Reads, Limits: c.Limits, Resolve: c.Resolve, Reranker: c.Reranker})
	if e != nil {
		return nil, e
	}
	d := c.Dependencies
	d.Plans = plans
	service, e := NewSearch(c.Profile, d)
	if e != nil {
		plans.Close()
		return nil, e
	}
	return &ConfiguredRuntime{plans: plans, service: service}, nil
}
func (r *ConfiguredRuntime) Search(ctx context.Context, in SearchInput) ([]contracts.Candidate, error) {
	if r == nil || r.closed.Load() {
		return nil, ErrSearch
	}
	return r.service.Search(ctx, in)
}
func (r *ConfiguredRuntime) Close() {
	if r == nil || !r.closed.CompareAndSwap(false, true) {
		return
	}
	r.plans.Close()
}
