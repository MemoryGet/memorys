// Package kernel is the controlled in-process module assembly layer.
// Its Config is a resolved assembly plan, NOT the external deployment schema.
// A production bootstrap must first validate deployment/profile/artifact rules.
package kernel

import (
	"context"
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type Manifest struct {
	ID                 string
	Version            string
	ContractVersion    string
	Capabilities       []contracts.Capability
	CapabilityVersions map[contracts.Capability]string
	CapabilityFeatures map[contracts.Capability][]string
	Dependencies       []contracts.Capability // Simple resolved-plan dependency shorthand.
	DependencySlots    []Dependency
}

type Dependency struct {
	Slot            string
	Capability      contracts.Capability
	ContractVersion string
	Features        []string
}

type Lookup func(slot string) (any, error)

type Factory struct {
	Manifest Manifest
	Validate func(json.RawMessage) error
	Create   func(context.Context, json.RawMessage, Lookup) (*Instance, error)
}

type Instance struct {
	Services map[contracts.Capability]any
	Health   func(context.Context) error
	Close    func(context.Context) error
}

type ModuleConfig struct {
	ID                    string            `json:"id"`
	Provider              string            `json:"provider"`
	ImplementationVersion string            `json:"implementation_version,omitempty"`
	Settings              json.RawMessage   `json:"settings"`
	DependencyBindings    map[string]string `json:"dependency_bindings,omitempty"`
}

type Config struct {
	Version  int                             `json:"version"`
	Modules  []ModuleConfig                  `json:"modules"`
	Bindings map[contracts.Capability]string `json:"bindings"`
}

type Requirement struct {
	Capability      contracts.Capability
	ContractVersion string
	Features        []string
}

type ModuleSnapshot struct {
	ID                    string
	Provider              string
	ImplementationVersion string
	ContractVersion       string
	Capabilities          []contracts.Capability
	CapabilityVersions    map[contracts.Capability]string
	CapabilityFeatures    map[contracts.Capability][]string
}

type Snapshot struct {
	ConfigVersion string
	Modules       []ModuleSnapshot
	Bindings      map[contracts.Capability]string
}
