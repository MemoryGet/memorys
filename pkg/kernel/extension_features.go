package kernel

import (
	_ "embed"
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// Compiled policy, not a deployment-controlled file or proof of actual behavior.
//
//go:embed extension-features.v1.json
var extensionPolicy []byte

var extensionMinimums = func() map[contracts.Capability][]string {
	var policy struct {
		Version      int                               `json:"schema_version"`
		Capabilities map[contracts.Capability][]string `json:"capabilities"`
	}
	if json.Unmarshal(extensionPolicy, &policy) != nil || policy.Version != 1 || len(policy.Capabilities) != 2 {
		panic("invalid compiled extension policy")
	}
	for cap, features := range policy.Capabilities {
		if (cap != contracts.MemoryStorage && cap != contracts.GraphStorage) || len(features) == 0 {
			panic("invalid compiled extension policy")
		}
		seen := map[string]bool{}
		for _, feature := range features {
			if !identifier.MatchString(feature) || seen[feature] {
				panic("invalid compiled extension policy")
			}
			seen[feature] = true
		}
	}
	return policy.Capabilities
}()

func MinimumExtensionFeatures(cap contracts.Capability) []string {
	return append([]string(nil), extensionMinimums[cap]...)
}
