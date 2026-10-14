package kernel_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

func TestInvalidFeatureDeclarationsRefuseBeforeCreation(t *testing.T) {
	for _, features := range [][]string{{""}, {"contains space"}, {strings.Repeat("a", 101)}, {"future.optional", "future.optional"}, {string([]byte{0xff})}} {
		var closed []string
		created := false
		factory := parserFactory("parser", &closed)
		factory.Manifest.CapabilityFeatures = map[contracts.Capability][]string{contracts.Parsing: features}
		create := factory.Create
		factory.Create = func(c context.Context, b json.RawMessage, l kernel.Lookup) (*kernel.Instance, error) {
			created = true
			return create(c, b, l)
		}
		_, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory})
		if err == nil || created {
			t.Fatal("invalid feature declaration accepted", features, err, created)
		}
	}
}

func TestInvalidFeatureRequirementsCannotMatchMalformedDeclarations(t *testing.T) {
	var closed []string
	factory := parserFactory("parser", &closed)
	factory.Manifest.CapabilityFeatures = map[contracts.Capability][]string{contracts.Parsing: {"future.optional"}}
	for _, features := range [][]string{{""}, {"future.optional", "future.optional"}} {
		if r, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory}, kernel.Requirement{Capability: contracts.Parsing, Features: features}); err == nil {
			_ = r.Close(context.Background())
			t.Fatal("malformed requirement accepted")
		}
	}
	r, err := kernel.Build(context.Background(), configuration("parser"), []kernel.Factory{factory}, kernel.Requirement{Capability: contracts.Parsing, Features: []string{"future.optional"}})
	if err != nil {
		t.Fatal("well-formed new feature rejected", err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
