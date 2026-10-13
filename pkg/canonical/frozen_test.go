package canonical_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/canonical"
)

func TestFrozenGoldenBytesAndHashes(t *testing.T) {
	data, err := os.ReadFile("../../docs/architecture/examples/frozen-content-golden.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		Cases []struct {
			Name      string                 `json:"name"`
			Input     string                 `json:"input_json"`
			Canonical string                 `json:"canonical_json"`
			Hashes    canonical.FrozenHashes `json:"hashes"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &suite); err != nil || len(suite.Cases) != 7 {
		t.Fatal("missing golden cases", err)
	}
	for _, test := range suite.Cases {
		t.Run(test.Name, func(t *testing.T) {
			encoded, err := canonical.JSON([]byte(test.Input))
			if err != nil || string(encoded) != test.Canonical {
				t.Fatal("cross-language bytes mismatch", err)
			}
			actual, err := canonical.Frozen([]byte(test.Input))
			if err != nil || actual != test.Hashes {
				t.Fatal("cross-language hashes mismatch", err)
			}
		})
	}
}

func TestFrozenEnvelopeRejectsUnknownFormatAndFields(t *testing.T) {
	for _, input := range []string{`null`, `[]`, `{}`, `{"format":"unknown","blocks":[{}],"blobs":[]}`,
		`{"format":"akos.freeze/1","blocks":[],"blobs":[]}`, `{"format":"akos.freeze/1","blocks":[{}],"blobs":null}`,
		`{"format":"akos.freeze/1","blocks":[{}],"blobs":[],"supplier_id":"x"}`, `{"x":`} {
		if _, err := canonical.Frozen([]byte(input)); err == nil {
			t.Fatal("accepted invalid frozen envelope")
		}
	}
}

func TestCanonicalLimitsAndEscapedSurrogatePair(t *testing.T) {
	for _, input := range []string{strings.Repeat(" ", (4<<20)+1), strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65),
		`[1` + strings.Repeat("0", 256) + `]`, `[1e` + strings.Repeat("1", 1024) + `]`, `[100e10000]`} {
		if _, err := canonical.JSON([]byte(input)); err == nil {
			t.Fatal("accepted unbounded input")
		}
	}
	actual, err := canonical.JSON([]byte(`"\ud83d\ude00"`))
	if err != nil || string(actual) != `"😀"` {
		t.Fatal("valid surrogate pair rejected", err)
	}
}
