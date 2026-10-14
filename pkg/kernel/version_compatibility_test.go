package kernel_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

func TestSameMajorNewerMinorContractsAssemble(t *testing.T) {
	for _, tc := range []struct{ name, lifecycle, supplied, requested string }{
		{"lifecycle-minor", "1.1", "1.0", "1.0"},
		{"lifecycle-double-digit", "1.10", "1.0", "1.0"},
		{"legacy-zero-patch", "1.1.0", "1.2.0", "1.1.0"},
		{"large-minor-no-host-overflow", "1.0", "1." + strings.Repeat("9", 40), "1.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var closed []string
			factory := parserFactory("parser", &closed)
			factory.Manifest.ContractVersion = tc.lifecycle
			factory.Manifest.CapabilityVersions = map[contracts.Capability]string{contracts.Parsing: tc.supplied}
			r, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory}, kernel.Requirement{Capability: contracts.Parsing, ContractVersion: tc.requested})
			if err != nil {
				t.Fatal("compatible declared version rejected", err)
			}
			if _, err := kernel.Resolve[contracts.Parser](r, contracts.Parsing); err != nil {
				t.Fatal(err)
			}
			if err := r.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIncompatibleAndInsufficientVersionsRefuseBeforeCreation(t *testing.T) {
	for _, tc := range []struct{ lifecycle, supplied, requested string }{
		{"2.0", "1.0", "1.0"},
		{"1.0", "2.0", "1.0"},
		{"1.0", "1.1", "1.2"},
		{"1.0", "1.01", "1.0"},
		{"1.0", "1.0.1", "1.0"},
	} {
		var closed []string
		factory := parserFactory("parser", &closed)
		factory.Manifest.ContractVersion = tc.lifecycle
		factory.Manifest.CapabilityVersions = map[contracts.Capability]string{contracts.Parsing: tc.supplied}
		created := false
		create := factory.Create
		factory.Create = func(c context.Context, b json.RawMessage, l kernel.Lookup) (*kernel.Instance, error) {
			created = true
			return create(c, b, l)
		}
		_, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory}, kernel.Requirement{Capability: contracts.Parsing, ContractVersion: tc.requested})
		if err == nil || created {
			t.Fatal("incompatible version admitted", tc, err, created)
		}
	}
}
