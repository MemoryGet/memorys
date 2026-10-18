package buildlock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func fixture(t *testing.T) (Input, Key) {
	t.Helper()
	key := Key{Provider: "fixture.provider", Version: "1.2.3"}
	deployment := []byte(`{"schema_version":1,"build_lock_ref":"lock-v1","modules":[{"instance_id":"fixture","provider":"fixture.provider","implementation_version":"1.2.3","contract":"1.0","settings":{},"dependency_bindings":{}}],"bindings":{},"index_profiles":[],"query_profiles":[],"pipeline_profiles":[]}`)
	retention := []byte(`{"schema_version":1,"policy_id":"policy","policy_version":1,"rollback_window_seconds":1,"replay_budget_seconds":1,"recovery_margin_seconds":1,"journal_min_seconds":1,"original_min_seconds":1,"audit_min_seconds":1,"idempotency_min_seconds":1,"retry_window_seconds":1,"max_attempt_seconds":1,"cancel_settle_seconds":1,"orphan_grace_seconds":1}`)
	manifest := []byte(`{"schema_version":1,"module_id":"fixture.provider","implementation_version":"1.2.3","contract_version":"1.0","capabilities":[],"dependencies":[],"config_schema":{},"health":{},"state_model":"stateless","migration_modes":[],"composite_groups":[],"limitations":[]}`)
	artifact := []byte("fixture artifact bytes")
	lockValue := map[string]any{
		"schema_version":    1,
		"lock_id":           "lock-v1",
		"deployment_sha256": sha(deployment),
		"retention_policy":  map[string]any{"policy_id": "policy", "policy_version": 1, "file_sha256": sha(retention)},
		"releases":          []any{map[string]any{"provider": key.Provider, "implementation_version": key.Version, "lifecycle_contract": "1.0", "manifest_sha256": sha(manifest), "artifact_sha256": sha(artifact), "source": map[string]any{"repository": "https://example.invalid/repo", "revision": strings.Repeat("a", 40)}}},
	}
	lock, err := json.Marshal(lockValue)
	if err != nil {
		t.Fatal(err)
	}
	return Input{Deployment: deployment, Lock: lock, Retention: retention, Manifests: map[Key][]byte{key: manifest}, Artifacts: map[Key]io.Reader{key: bytes.NewReader(artifact)}}, key
}

func TestVerifyLockedInputsBindsRawDigestsAndCoverage(t *testing.T) {
	input, key := fixture(t)
	report, err := Verify(input)
	if err != nil {
		t.Fatal(err)
	}
	if report.ReadyForService || report.ManifestSHA256[key] == "" || report.ArtifactSHA256[key] == "" || len(report.Unverified) == 0 || !report.CoversRuntimeModules([]Key{key}) {
		t.Fatal("unexpected build lock report", report)
	}
	if report.CoversRuntimeModules([]Key{key, key}) || report.CoversRuntimeModules(nil) {
		t.Fatal("runtime module coverage accepted duplicate or missing pair")
	}
}

func TestVerifyRejectsArtifactAndStructuralChanges(t *testing.T) {
	input, key := fixture(t)
	input.Artifacts = map[Key]io.Reader{key: bytes.NewReader([]byte("tampered"))}
	if _, err := Verify(input); !errors.Is(err, ErrVerification) {
		t.Fatal("artifact mismatch accepted", err)
	}
	input, _ = fixture(t)
	input.Deployment = append(input.Deployment, []byte(" {}")...)
	if _, err := Verify(input); !errors.Is(err, ErrVerification) {
		t.Fatal("trailing deployment JSON accepted", err)
	}
	input, _ = fixture(t)
	input.Lock = append(input.Lock, []byte(`,"unexpected":true}`)...)
	if _, err := Verify(input); !errors.Is(err, ErrVerification) {
		t.Fatal("unknown lock field accepted", err)
	}
}

func TestVerifyFilesUsesExplicitPaths(t *testing.T) {
	input, key := fixture(t)
	dir := t.TempDir()
	paths := map[string]string{}
	for name, raw := range map[string][]byte{"deployment.json": input.Deployment, "lock.json": input.Lock, "retention.json": input.Retention, "manifest.json": input.Manifests[key]} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	artifactPath := filepath.Join(dir, "artifact.bin")
	if err := os.WriteFile(artifactPath, []byte("fixture artifact bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyFiles(paths["deployment.json"], paths["lock.json"], paths["retention.json"], map[Key]string{key: paths["manifest.json"]}, map[Key]string{key: artifactPath})
	if err != nil || report.LockSHA256 == "" {
		t.Fatal("explicit file verification failed", err)
	}
}
