package kernel

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// Load is for internal assembly-plan diagnostics, not deployment ingestion.
// It does not validate external profile, migration or artifact-lock semantics.
func Load(reader io.Reader) (Config, error) {
	data, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return Config{}, errors.New("assembly plan could not be read")
	}
	if err := validateJSONObject(data); err != nil {
		return Config{}, errors.New("invalid assembly plan JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, errors.New("invalid assembly plan fields")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Config{}, errors.New("trailing assembly plan data")
	}
	return config, nil
}

func cloneConfig(config Config) Config {
	result := config
	result.Bindings = copyMap(config.Bindings)
	result.Modules = append([]ModuleConfig(nil), config.Modules...)
	for i := range result.Modules {
		result.Modules[i].Settings = append(json.RawMessage(nil), result.Modules[i].Settings...)
		result.Modules[i].DependencyBindings = copyMap(result.Modules[i].DependencyBindings)
	}
	return result
}

func copyMap[K comparable, V any](input map[K]V) map[K]V {
	result := make(map[K]V, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneManifest(manifest Manifest) Manifest {
	result := manifest
	result.Capabilities = append([]contracts.Capability(nil), manifest.Capabilities...)
	result.Dependencies = append([]contracts.Capability(nil), manifest.Dependencies...)
	result.CapabilityVersions = copyMap(manifest.CapabilityVersions)
	result.CapabilityFeatures = copyMap(manifest.CapabilityFeatures)
	for cap, features := range result.CapabilityFeatures {
		result.CapabilityFeatures[cap] = append([]string(nil), features...)
	}
	result.DependencySlots = append([]Dependency(nil), manifest.DependencySlots...)
	for i := range result.DependencySlots {
		result.DependencySlots[i].Features = append([]string(nil), result.DependencySlots[i].Features...)
	}
	return result
}
