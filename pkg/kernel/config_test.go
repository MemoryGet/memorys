package kernel_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/kernel"
)

func TestDirectAssemblyRejectsDuplicateSettingsBeforeCreation(t *testing.T) {
	var closed []string
	factory := parserFactory("parser", &closed)
	created := false
	original := factory.Create
	factory.Create = func(ctx context.Context, settings json.RawMessage, lookup kernel.Lookup) (*kernel.Instance, error) {
		created = true
		return original(ctx, settings, lookup)
	}
	config := configuration("parser")
	config.Modules[0].Settings = json.RawMessage(`{"nested":{"key":1,"key":2}}`)
	if _, err := kernel.Build(context.Background(), config, []kernel.Factory{factory}); err == nil || created {
		t.Fatal("ambiguous settings reached a provider")
	}
}

func TestConfigDecoderAcceptsNestedObjectsAndArrays(t *testing.T) {
	input := `{"version":1,"modules":[{"id":"p","provider":"p","settings":{"items":[{"x":1},{"x":2}],"nullable":null}}],"bindings":{}}`
	if _, err := kernel.Load(strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
}

func TestConfigDecoderRejectsUnknownFieldsAndTrailingData(t *testing.T) {
	for _, input := range []string{
		`{"version":1,"modules":[],"bindings":{},"typo":true}`,
		`{"version":1,"modules":[],"bindings":{}} {}`,
		`{"version":1,"modules":[{"id":"p","provider":"p","setting":{}}],"bindings":{}}`,
	} {
		if _, err := kernel.Load(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted malformed config: %s", input)
		}
	}
}

func TestConfigDecoderRejectsAmbiguousJSON(t *testing.T) {
	for _, input := range []string{
		`null`,
		`{"version":1,"version":2,"modules":[],"bindings":{}}`,
		`{"version":1,"modules":[{"id":"p","provider":"p","settings":{"x":1,"x":2}}],"bindings":{}}`,
		`{"version":1,"modules":[],"bindings":{"parser":"a","parser":"b"}}`,
		strings.Repeat(" ", (4<<20)+1),
		`{"x":` + strings.Repeat(`[`, 65) + `0` + strings.Repeat(`]`, 65) + `}`,
	} {
		if _, err := kernel.Load(strings.NewReader(input)); err == nil {
			t.Fatal("accepted ambiguous or oversized JSON")
		}
	}
}
