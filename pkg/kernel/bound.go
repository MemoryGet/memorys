package kernel

import (
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// GetBound resolves a named instance rather than the deployment's default for
// a capability. This is internal lookup, not a caller authorization boundary.
func (r *Runtime) GetBound(b contracts.ModuleBinding) (any, error) {
	if r == nil {
		return nil, errors.New("module runtime is missing")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("module runtime is closed")
	}
	instance := r.instances[string(b.InstanceID)]
	if instance == nil {
		return nil, errors.New("profile instance missing")
	}
	for _, m := range r.snapshot.Modules {
		if m.ID == string(b.InstanceID) {
			manifest := Manifest{Capabilities: m.Capabilities, CapabilityVersions: m.CapabilityVersions, CapabilityFeatures: m.CapabilityFeatures}
			if b.ContractVersion == "" || !supports(manifest, Requirement{Capability: b.Capability, ContractVersion: b.ContractVersion, Features: b.RequiredFeatures}) {
				return nil, errors.New("profile binding unsupported")
			}
			value := instance.Services[b.Capability]
			if nilService(value) {
				return nil, errors.New("profile service missing")
			}
			return value, nil
		}
	}
	return nil, errors.New("profile instance metadata missing")
}
