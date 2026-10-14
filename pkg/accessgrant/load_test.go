package accessgrant_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/accessgrant"
)

func keyGolden(t *testing.T) (string, accessgrant.SnapshotIdentity) {
	t.Helper()
	data, err := os.ReadFile("../../docs/architecture/examples/key-snapshot-golden.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var sample struct {
		Config   string                       `json:"config_json"`
		Expected accessgrant.SnapshotIdentity `json:"expected"`
	}
	if err := json.Unmarshal(data, &sample); err != nil {
		t.Fatal(err)
	}
	return sample.Config, sample.Expected
}

func TestJSONSnapshotMatchesIndependentPythonIdentity(t *testing.T) {
	raw, want := keyGolden(t)
	v, err := accessgrant.LoadVerifier([]byte(raw), want)
	if err != nil || v.Identity() != want || want.Version != 9007199254740993 {
		t.Fatal("Python/Go snapshot identity mismatch", err)
	}
	for _, mutate := range []func(*accessgrant.SnapshotIdentity){func(e *accessgrant.SnapshotIdentity) { e.ID = "other" }, func(e *accessgrant.SnapshotIdentity) { e.Version-- }, func(e *accessgrant.SnapshotIdentity) { e.Digest = strings.Repeat("f", 64) }} {
		e := want
		mutate(&e)
		if got, err := accessgrant.LoadVerifier([]byte(raw), e); err != accessgrant.ErrInvalid || got != nil {
			t.Fatal("independent expectation mismatch accepted")
		}
	}
}

func TestJSONSnapshotRejectsMissingDuplicateFloatAndNullFields(t *testing.T) {
	raw, want := keyGolden(t)
	for _, bad := range []string{strings.Replace(raw, `"valid_from_ns":0`, `"valid_from_ns":null`, 1), strings.Replace(raw, `"valid_from_ns":0`, `"valid_from_ns":0.0`, 1), strings.Replace(raw, `"valid_from_ns":0,`, ``, 1), strings.Replace(raw, `"version":9007199254740993`, `"version":9007199254740993,"version":9007199254740993`, 1), strings.Replace(raw, `"snapshot_id"`, `"SNAPSHOT_ID"`, 1), raw + ` {}`} {
		if got, err := accessgrant.LoadVerifier([]byte(bad), want); err != accessgrant.ErrInvalid || got != nil {
			t.Fatal("malformed snapshot accepted")
		}
	}
}

func TestJSONSnapshotRejectsProviderFieldsAndAlternativeKeyEncoding(t *testing.T) {
	raw, want := keyGolden(t)
	numbers := make([]int, 32)
	for i := range numbers {
		numbers[i] = i
	}
	array, _ := json.Marshal(numbers)
	for _, bad := range []string{strings.Replace(raw, `"algorithm":"EdDSA"`, `"algorithm":"EdDSA","download_url":"https://invalid.example/key"`, 1), strings.Replace(raw, `"public_key":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="`, `"public_key":`+string(array), 1), strings.Replace(raw, `GBkaGxwdHh8=`, `GBkaGxwdHh9=`, 1)} {
		if _, err := accessgrant.LoadVerifier([]byte(bad), want); err == nil {
			t.Fatal("alternative/unsafe key encoding accepted")
		}
	}
}
