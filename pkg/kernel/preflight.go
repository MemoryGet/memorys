package kernel

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,99}$`)
var release = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var contract = regexp.MustCompile(`^[1-9][0-9]*\.(0|[1-9][0-9]*)(\.0)?$`)

type plannedModule struct {
	config       ModuleConfig
	factory      Factory
	dependencies map[string]dependencyTarget
}

type dependencyTarget struct {
	instance   string
	capability contracts.Capability
}

func compatible(supplied, requested string) bool {
	if requested == "" {
		requested = "1.0"
	}
	if !contract.MatchString(supplied) || !contract.MatchString(requested) {
		return false
	}
	s, r := strings.Split(supplied, "."), strings.Split(requested, ".")
	// Regex guarantees canonical decimal components without leading zeroes.
	// Compare numerals without host-sized int parsing or overflow.
	minorAtLeast := len(s[1]) > len(r[1]) || (len(s[1]) == len(r[1]) && s[1] >= r[1])
	return s[0] == r[0] && minorAtLeast
}

func capabilityVersion(manifest Manifest, cap contracts.Capability) string {
	if value := manifest.CapabilityVersions[cap]; value != "" {
		return value
	}
	return "1.0"
}

func supports(manifest Manifest, requirement Requirement) bool {
	if !validFeatures(requirement.Features) {
		return false
	}
	declared := false
	for _, cap := range manifest.Capabilities {
		if cap == requirement.Capability {
			declared = true
		}
	}
	if !declared || !compatible(capabilityVersion(manifest, requirement.Capability), requirement.ContractVersion) {
		return false
	}
	features := manifest.CapabilityFeatures[requirement.Capability]
	if !validFeatures(features) {
		return false
	}
	needed := append(MinimumExtensionFeatures(requirement.Capability), requirement.Features...)
	for _, required := range needed {
		found := false
		for _, feature := range features {
			if feature == required {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func validFeatures(features []string) bool {
	seen := make(map[string]bool, len(features))
	for _, feature := range features {
		if !identifier.MatchString(feature) || seen[feature] {
			return false
		}
		seen[feature] = true
	}
	return true
}

func validateManifest(manifest Manifest) error {
	if !identifier.MatchString(manifest.ID) || !release.MatchString(manifest.Version) || !compatible(manifest.ContractVersion, "1.0") {
		return errors.New("module implementation or lifecycle contract is invalid")
	}
	if len(manifest.Capabilities) == 0 {
		return errors.New("module declares no capabilities")
	}
	seen := make(map[contracts.Capability]bool)
	for _, cap := range manifest.Capabilities {
		if portChecks[cap] == nil || seen[cap] || !compatible(capabilityVersion(manifest, cap), "1.0") {
			return errors.New("invalid module capability declaration")
		}
		seen[cap] = true
		if !supports(manifest, Requirement{Capability: cap}) {
			return errors.New("module lacks minimum capability features")
		}
	}
	for cap := range manifest.CapabilityFeatures {
		if !seen[cap] {
			return errors.New("features for undeclared capability")
		}
	}
	for cap := range manifest.CapabilityVersions {
		if !seen[cap] {
			return errors.New("version for undeclared capability")
		}
	}
	return nil
}

func selectFactory(module ModuleConfig, factories []Factory) (Factory, error) {
	var selected []Factory
	for _, factory := range factories {
		if factory.Manifest.ID == module.Provider && (module.ImplementationVersion == "" || factory.Manifest.Version == module.ImplementationVersion) {
			selected = append(selected, factory)
		}
	}
	if len(selected) != 1 {
		return Factory{}, errors.New("module provider release is missing or ambiguous")
	}
	factory := selected[0]
	factory.Manifest = cloneManifest(factory.Manifest)
	if err := validateManifest(factory.Manifest); err != nil {
		return Factory{}, err
	}
	if factory.Create == nil || factory.Validate == nil {
		return Factory{}, errors.New("module factory is incomplete")
	}
	return factory, nil
}

func canonicalSettings(settings json.RawMessage) (json.RawMessage, error) {
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}
	if err := validateJSONObject(settings); err != nil {
		return nil, errors.New("invalid module settings JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(settings))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, errors.New("module settings must be an object")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, errors.New("trailing module settings")
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, errors.New("invalid module settings")
	}
	return canonical, nil
}

func safeValidate(factory Factory, settings json.RawMessage) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("module settings validation failed")
		}
	}()
	if factory.Validate(append(json.RawMessage(nil), settings...)) != nil {
		return errors.New("module settings failed validation")
	}
	return nil
}

func planModules(config Config, factories []Factory) (map[string]*plannedModule, error) {
	if config.Version != 1 || len(config.Modules) == 0 {
		return nil, errors.New("invalid assembly plan version or module set")
	}
	modules := make(map[string]*plannedModule)
	for _, module := range config.Modules {
		if !identifier.MatchString(module.ID) || modules[module.ID] != nil {
			return nil, errors.New("invalid or duplicate module instance")
		}
		factory, err := selectFactory(module, factories)
		if err != nil {
			return nil, err
		}
		module.ImplementationVersion = factory.Manifest.Version
		module.Settings, err = canonicalSettings(module.Settings)
		if err != nil {
			return nil, err
		}
		if err := safeValidate(factory, module.Settings); err != nil {
			return nil, err
		}
		modules[module.ID] = &plannedModule{config: module, factory: factory, dependencies: make(map[string]dependencyTarget)}
	}
	return modules, nil
}

func bindDependencies(modules map[string]*plannedModule, config Config) error {
	for _, module := range modules {
		declarations := append([]Dependency(nil), module.factory.Manifest.DependencySlots...)
		for _, cap := range module.factory.Manifest.Dependencies {
			declarations = append(declarations, Dependency{Slot: string(cap), Capability: cap})
		}
		graph, manager := false, false
		for _, cap := range module.factory.Manifest.Capabilities {
			graph = graph || cap == contracts.GraphStorage
			manager = manager || cap == contracts.GraphProjectionControl
		}
		if graph && !manager {
			declared := false
			for _, dep := range declarations {
				if dep.Slot == "projection_manager" {
					if dep.Capability != contracts.GraphProjectionControl {
						return errors.New("graph management slot has incompatible capability")
					}
					declared = true
				}
			}
			if !declared {
				declarations = append(declarations, Dependency{Slot: "projection_manager", Capability: contracts.GraphProjectionControl, ContractVersion: "1.0"})
			}
		}
		for _, dep := range declarations {
			if !identifier.MatchString(dep.Slot) || module.dependencies[dep.Slot].instance != "" {
				return errors.New("invalid dependency slot")
			}
			id := module.config.DependencyBindings[dep.Slot]
			if id == "" {
				id = config.Bindings[dep.Capability]
			}
			target := modules[id]
			if target == nil || !supports(target.factory.Manifest, Requirement{Capability: dep.Capability, ContractVersion: dep.ContractVersion, Features: dep.Features}) {
				return errors.New("module dependency is unavailable or incompatible")
			}
			module.dependencies[dep.Slot] = dependencyTarget{instance: id, capability: dep.Capability}
		}
		for slot := range module.config.DependencyBindings {
			if module.dependencies[slot].instance == "" {
				return errors.New("undeclared dependency binding")
			}
		}
	}
	return nil
}

func orderModules(modules map[string]*plannedModule) ([]string, error) {
	remaining := make(map[string]map[string]bool)
	for id, module := range modules {
		remaining[id] = make(map[string]bool)
		for _, dep := range module.dependencies {
			remaining[id][dep.instance] = true
		}
	}
	var order []string
	for len(remaining) > 0 {
		var ready []string
		for id, deps := range remaining {
			if len(deps) == 0 {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			return nil, errors.New("module dependency cycle")
		}
		sort.Strings(ready)
		for _, id := range ready {
			order = append(order, id)
			delete(remaining, id)
			for _, deps := range remaining {
				delete(deps, id)
			}
		}
	}
	return order, nil
}

func preflight(config Config, factories []Factory, requirements []Requirement) (map[string]*plannedModule, []string, error) {
	modules, err := planModules(config, factories)
	if err != nil {
		return nil, nil, err
	}
	if len(config.Bindings) == 0 {
		return nil, nil, errors.New("assembly plan has no bindings")
	}
	for cap, id := range config.Bindings {
		module := modules[id]
		if module == nil || portChecks[cap] == nil || !supports(module.factory.Manifest, Requirement{Capability: cap}) {
			return nil, nil, errors.New("invalid capability binding")
		}
	}
	for _, req := range requirements {
		module := modules[config.Bindings[req.Capability]]
		if module == nil || !supports(module.factory.Manifest, req) {
			return nil, nil, errors.New("required capability or feature is missing")
		}
	}
	if err := bindDependencies(modules, config); err != nil {
		return nil, nil, err
	}
	order, err := orderModules(modules)
	return modules, order, err
}
