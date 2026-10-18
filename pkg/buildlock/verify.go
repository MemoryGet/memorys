// Package buildlock validates one deployment's locked module inputs.
// Verification is offline and never executes or downloads an artifact.
package buildlock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/canonical"
)

var ErrVerification = errors.New("build lock verification rejected")

const (
	maxJSONBytes     = 4 << 20
	maxArtifactBytes = 4 << 30
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,99}$`)
var releasePattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var contractPattern = regexp.MustCompile(`^[1-9][0-9]*\.(0|[1-9][0-9]*)$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var revisionPattern = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

type Key struct {
	Provider string
	Version  string
}

type Input struct {
	Deployment []byte
	Lock       []byte
	Retention  []byte
	Manifests  map[Key][]byte
	Artifacts  map[Key]io.Reader
}

type Report struct {
	DeploymentSHA256 string
	LockSHA256       string
	RetentionSHA256  string
	ManifestSHA256   map[Key]string
	ArtifactSHA256   map[Key]string
	ReadyForService  bool
	Unverified       []string
}

type lockRelease struct {
	key              Key
	lifecycle        string
	manifestDigest   string
	artifactDigest   string
	sourceRepository string
	sourceRevision   string
}

func Verify(input Input) (Report, error) {
	var report Report
	if len(input.Deployment) == 0 || len(input.Deployment) > maxJSONBytes || len(input.Lock) == 0 || len(input.Lock) > maxJSONBytes || len(input.Retention) == 0 || len(input.Retention) > maxJSONBytes || len(input.Manifests) == 0 || len(input.Artifacts) == 0 {
		return report, ErrVerification
	}
	deployment, err := parseObject(input.Deployment, deploymentFields)
	if err != nil {
		return report, err
	}
	if err = requireFields(deployment, deploymentRequiredFields); err != nil {
		return report, err
	}
	lock, err := parseObject(input.Lock, lockFields)
	if err != nil {
		return report, err
	}
	if err = requireFields(lock, lockFields); err != nil {
		return report, err
	}
	retention, err := parseObject(input.Retention, retentionFields)
	if err != nil {
		return report, err
	}
	if err = requireFields(retention, retentionFields); err != nil {
		return report, err
	}
	if err = requireInt(deployment, "schema_version", 1, false); err != nil {
		return report, err
	}
	if err = requireInt(lock, "schema_version", 1, false); err != nil {
		return report, err
	}
	if err = requireInt(retention, "schema_version", 1, false); err != nil {
		return report, err
	}
	buildLockRef, err := requiredString(deployment, "build_lock_ref", identifierPattern)
	if err != nil {
		return report, err
	}
	lockID, err := requiredString(lock, "lock_id", identifierPattern)
	if err != nil || buildLockRef != lockID {
		return report, ErrVerification
	}
	deploymentDigest := digestBytes(input.Deployment)
	lockedDeploymentDigest, err := requiredDigest(lock, "deployment_sha256")
	if err != nil || deploymentDigest != lockedDeploymentDigest {
		return report, ErrVerification
	}
	retentionID, err := requiredString(retention, "policy_id", identifierPattern)
	if err != nil {
		return report, err
	}
	retentionVersion, err := requiredInt(retention, "policy_version", true)
	if err != nil {
		return report, err
	}
	for _, field := range retentionSeconds {
		if _, err = requiredInt(retention, field, true); err != nil {
			return report, err
		}
	}
	retentionDigest := digestBytes(input.Retention)
	lockedRetention, err := object(lock, "retention_policy", retentionPolicyFields)
	if err != nil {
		return report, err
	}
	policyID, err := requiredString(lockedRetention, "policy_id", identifierPattern)
	if err != nil {
		return report, err
	}
	policyVersion, err := requiredInt(lockedRetention, "policy_version", true)
	if err != nil {
		return report, err
	}
	policyDigest, err := requiredDigest(lockedRetention, "file_sha256")
	if err != nil || policyID != retentionID || policyVersion != retentionVersion || policyDigest != retentionDigest {
		return report, ErrVerification
	}

	modules, err := deploymentModules(deployment)
	if err != nil {
		return report, err
	}
	releases, err := lockReleases(lock)
	if err != nil {
		return report, err
	}
	if !sameKeys(modules, releases) || !sameKeys(modules, input.Manifests) || !sameKeys(modules, input.Artifacts) {
		return report, ErrVerification
	}
	report.ManifestSHA256 = make(map[Key]string, len(input.Manifests))
	for key, raw := range input.Manifests {
		manifest, err := parseObject(raw, manifestFields)
		if err != nil {
			return report, err
		}
		if err = requireFields(manifest, manifestRequiredFields); err != nil {
			return report, err
		}
		moduleID, err := requiredString(manifest, "module_id", identifierPattern)
		if err != nil {
			return report, err
		}
		version, err := requiredString(manifest, "implementation_version", releasePattern)
		if err != nil || moduleID != key.Provider || version != key.Version {
			return report, ErrVerification
		}
		contract, err := requiredString(manifest, "contract_version", contractPattern)
		if err != nil || releases[key].lifecycle != contract {
			return report, ErrVerification
		}
		digest := digestBytes(raw)
		if digest != releases[key].manifestDigest {
			return report, ErrVerification
		}
		report.ManifestSHA256[key] = digest
	}
	report.ArtifactSHA256 = make(map[Key]string, len(input.Artifacts))
	for key, reader := range input.Artifacts {
		if reader == nil {
			return report, ErrVerification
		}
		digest, err := digestReader(reader)
		if err != nil || digest != releases[key].artifactDigest {
			return report, ErrVerification
		}
		report.ArtifactSHA256[key] = digest
	}
	report.DeploymentSHA256 = deploymentDigest
	report.LockSHA256 = digestBytes(input.Lock)
	report.RetentionSHA256 = retentionDigest
	report.Unverified = []string{"trusted_lock_provenance", "source_build_attestation", "executed_artifact_identity", "backend_dependency_versions", "retention_holds"}
	return report, nil
}

// VerifyFiles reads only caller-supplied paths and closes opened artifacts.
// Paths are never taken from JSON input.
func VerifyFiles(deploymentPath, lockPath, retentionPath string, manifests map[Key]string, artifacts map[Key]string) (Report, error) {
	deployment, err := readJSON(deploymentPath)
	if err != nil {
		return Report{}, ErrVerification
	}
	lock, err := readJSON(lockPath)
	if err != nil {
		return Report{}, ErrVerification
	}
	retention, err := readJSON(retentionPath)
	if err != nil {
		return Report{}, ErrVerification
	}
	manifestBytes := make(map[Key][]byte, len(manifests))
	for key, path := range manifests {
		raw, readErr := readJSON(path)
		if readErr != nil {
			return Report{}, ErrVerification
		}
		manifestBytes[key] = raw
	}
	readers := make(map[Key]io.Reader, len(artifacts))
	files := make([]*os.File, 0, len(artifacts))
	for key, path := range artifacts {
		if path == "" {
			return Report{}, ErrVerification
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			for _, opened := range files {
				_ = opened.Close()
			}
			return Report{}, ErrVerification
		}
		files = append(files, file)
		readers[key] = file
	}
	report, verifyErr := Verify(Input{Deployment: deployment, Lock: lock, Retention: retention, Manifests: manifestBytes, Artifacts: readers})
	for _, file := range files {
		if closeErr := file.Close(); verifyErr == nil && closeErr != nil {
			verifyErr = ErrVerification
		}
	}
	if verifyErr != nil {
		return Report{}, ErrVerification
	}
	return report, nil
}

func readJSON(path string) ([]byte, error) {
	if path == "" {
		return nil, ErrVerification
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > maxJSONBytes {
		return nil, ErrVerification
	}
	return raw, nil
}

func digestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func digestReader(reader io.Reader) (string, error) {
	hash := sha256.New()
	limited := io.LimitReader(reader, maxArtifactBytes+1)
	count, err := io.CopyBuffer(hash, limited, make([]byte, 1<<20))
	if err != nil {
		return "", err
	}
	if count > maxArtifactBytes {
		return "", ErrVerification
	}
	sum := hash.Sum(nil)
	return hex.EncodeToString(sum), nil
}

func parseObject(raw []byte, allowed []string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxJSONBytes {
		return nil, ErrVerification
	}
	if _, err := canonical.JSON(raw); err != nil {
		return nil, ErrVerification
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, field := range allowed {
		allowedSet[field] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value map[string]json.RawMessage
	if decoder.Decode(&value) != nil || value == nil {
		return nil, ErrVerification
	}
	for field := range value {
		if !allowedSet[field] {
			return nil, ErrVerification
		}
	}
	return value, nil
}

func object(parent map[string]json.RawMessage, field string, allowed []string) (map[string]json.RawMessage, error) {
	raw, ok := parent[field]
	if !ok {
		return nil, ErrVerification
	}
	return parseObject(raw, allowed)
}

func requiredString(object map[string]json.RawMessage, field string, pattern *regexp.Regexp) (string, error) {
	raw, ok := object[field]
	if !ok {
		return "", ErrVerification
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || !pattern.MatchString(value) {
		return "", ErrVerification
	}
	return value, nil
}

func requireFields(object map[string]json.RawMessage, fields []string) error {
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			return ErrVerification
		}
	}
	return nil
}

func requiredDigest(object map[string]json.RawMessage, field string) (string, error) {
	return requiredString(object, field, digestPattern)
}

func requiredInt(object map[string]json.RawMessage, field string, positive bool) (int64, error) {
	raw, ok := object[field]
	if !ok {
		return 0, ErrVerification
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) != nil || number.String() == "" {
		return 0, ErrVerification
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || (positive && value < 1) {
		return 0, ErrVerification
	}
	return value, nil
}

func requireInt(object map[string]json.RawMessage, field string, expected int64, positive bool) error {
	value, err := requiredInt(object, field, positive)
	if err != nil || value != expected {
		return ErrVerification
	}
	return nil
}

func deploymentModules(deployment map[string]json.RawMessage) (map[Key]bool, error) {
	raw, ok := deployment["modules"]
	if !ok {
		return nil, ErrVerification
	}
	var modules []json.RawMessage
	if json.Unmarshal(raw, &modules) != nil || len(modules) == 0 {
		return nil, ErrVerification
	}
	result := make(map[Key]bool, len(modules))
	for _, moduleRaw := range modules {
		module, err := parseObject(moduleRaw, deploymentModuleFields)
		if err != nil {
			return nil, err
		}
		if err = requireFields(module, deploymentModuleRequiredFields); err != nil {
			return nil, err
		}
		provider, err := requiredString(module, "provider", identifierPattern)
		if err != nil {
			return nil, err
		}
		version, err := requiredString(module, "implementation_version", releasePattern)
		if err != nil {
			return nil, err
		}
		key := Key{Provider: provider, Version: version}
		if result[key] {
			return nil, ErrVerification
		}
		result[key] = true
	}
	return result, nil
}

func lockReleases(lock map[string]json.RawMessage) (map[Key]lockRelease, error) {
	raw, ok := lock["releases"]
	if !ok {
		return nil, ErrVerification
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
		return nil, ErrVerification
	}
	result := make(map[Key]lockRelease, len(values))
	for _, releaseRaw := range values {
		release, err := parseObject(releaseRaw, releaseFields)
		if err != nil {
			return nil, err
		}
		if err = requireFields(release, releaseFields); err != nil {
			return nil, err
		}
		provider, err := requiredString(release, "provider", identifierPattern)
		if err != nil {
			return nil, err
		}
		version, err := requiredString(release, "implementation_version", releasePattern)
		if err != nil {
			return nil, err
		}
		lifecycle, err := requiredString(release, "lifecycle_contract", contractPattern)
		if err != nil {
			return nil, err
		}
		manifestDigest, err := requiredDigest(release, "manifest_sha256")
		if err != nil {
			return nil, err
		}
		artifactDigest, err := requiredDigest(release, "artifact_sha256")
		if err != nil {
			return nil, err
		}
		source, err := object(release, "source", sourceFields)
		if err != nil {
			return nil, err
		}
		if err = requireFields(source, sourceFields); err != nil {
			return nil, err
		}
		repository, err := requiredNonEmptyString(source, "repository")
		if err != nil {
			return nil, err
		}
		revision, err := requiredString(source, "revision", revisionPattern)
		if err != nil {
			return nil, err
		}
		key := Key{Provider: provider, Version: version}
		if _, exists := result[key]; exists {
			return nil, ErrVerification
		}
		result[key] = lockRelease{key: key, lifecycle: lifecycle, manifestDigest: manifestDigest, artifactDigest: artifactDigest, sourceRepository: repository, sourceRevision: revision}
	}
	return result, nil
}

func requiredNonEmptyString(object map[string]json.RawMessage, field string) (string, error) {
	raw, ok := object[field]
	if !ok {
		return "", ErrVerification
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" || len(value) > 500 {
		return "", ErrVerification
	}
	return value, nil
}

func sameKeys[T any](expected map[Key]bool, actual map[Key]T) bool {
	if len(expected) != len(actual) {
		return false
	}
	for key := range expected {
		if _, ok := actual[key]; !ok {
			return false
		}
	}
	return true
}

var deploymentFields = []string{"schema_version", "build_lock_ref", "modules", "bindings", "index_profiles", "query_profiles", "pipeline_profiles"}
var deploymentRequiredFields = deploymentFields
var deploymentModuleFields = []string{"instance_id", "provider", "implementation_version", "contract", "settings", "credential_ref", "dependency_bindings"}
var deploymentModuleRequiredFields = []string{"instance_id", "provider", "implementation_version", "contract", "settings", "dependency_bindings"}
var lockFields = []string{"schema_version", "lock_id", "deployment_sha256", "retention_policy", "releases"}
var retentionPolicyFields = []string{"policy_id", "policy_version", "file_sha256"}
var retentionFields = append([]string{"schema_version", "policy_id", "policy_version"}, retentionSeconds...)
var releaseFields = []string{"provider", "implementation_version", "lifecycle_contract", "manifest_sha256", "artifact_sha256", "source"}
var sourceFields = []string{"repository", "revision"}
var manifestFields = []string{"schema_version", "module_id", "implementation_version", "contract_version", "capabilities", "dependencies", "config_schema", "health", "state_model", "migration_modes", "composite_groups", "limitations"}
var manifestRequiredFields = []string{"schema_version", "module_id", "implementation_version", "contract_version", "capabilities", "dependencies", "config_schema", "health", "state_model", "migration_modes", "composite_groups"}
var retentionSeconds = []string{"rollback_window_seconds", "replay_budget_seconds", "recovery_margin_seconds", "journal_min_seconds", "original_min_seconds", "audit_min_seconds", "idempotency_min_seconds", "retry_window_seconds", "max_attempt_seconds", "cancel_settle_seconds", "orphan_grace_seconds"}
