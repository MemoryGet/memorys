package modulewire_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func TestGrantDigestCrossLanguageGolden(t *testing.T) {
	data, err := os.ReadFile("../../docs/architecture/examples/grant-binding-golden.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	// Explicit wire keys keep the golden file independent from Go field naming.
	var fixture struct {
		Cases []struct {
			Name      string `json:"name"`
			Kind      string `json:"kind"`
			Input     string `json:"input_json"`
			Canonical string `json:"canonical_json"`
			Digest    string `json:"digest"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 6 {
		t.Fatal("invalid golden suite", err)
	}
	for _, sample := range fixture.Cases {
		t.Run(sample.Name, func(t *testing.T) {
			encoded, err := canonical.JSON([]byte(sample.Input))
			if err != nil || string(encoded) != sample.Canonical {
				t.Fatal("canonical byte mismatch", err)
			}
			digest, err := modulewire.GrantBindingDigest([]byte(sample.Input), sample.Kind)
			if err != nil || string(digest) != sample.Digest {
				t.Fatal("grant digest mismatch", err)
			}
			if sample.Name == "typed_parser_request" {
				if _, _, err := modulewire.DecodeParseRequest([]byte(sample.Input)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestGrantDigestRejectsAmbiguousInputsAndSeparatesDomains(t *testing.T) {
	for _, input := range [][]byte{[]byte(`null`), []byte(`[]`), []byte(`{"x":1,"x":2}`), []byte(`{} {}`), []byte("{\"x\":\"\xff\"}")} {
		if _, err := modulewire.GrantBindingDigest(input, "request"); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	if _, err := modulewire.GrantBindingDigest([]byte(`{}`), "supplier"); err == nil {
		t.Fatal("unknown domain accepted")
	}
	a, _ := modulewire.GrantBindingDigest([]byte(`{"n":9007199254740993}`), "request")
	b, _ := modulewire.GrantBindingDigest([]byte(`{"n":9007199254740993}`), "scope")
	c, _ := modulewire.GrantBindingDigest([]byte(`{"n":9007199254740992}`), "request")
	if a == b || a == c {
		t.Fatal("domain or exact integer changed without digest change")
	}
}
