package bootstrap

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/buildlock"
	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/events"
	"github.com/MemoryGet/memorys/pkg/identitytoken"
	"github.com/MemoryGet/memorys/pkg/kernel"
)

var ErrProductionConfig = errors.New("invalid production bootstrap configuration")

// ProductionSettings is the non-secret deployment contract. DatabaseURL may
// contain credentials supplied by the deployment secret manager; callers must
// never log or serialize this struct.
type ProductionSettings struct {
	ListenAddr        string
	DatabaseURL       string
	Issuer            string
	Audience          string
	JWKSFile          string
	ProfileFile       string
	ModuleCatalogFile string
}

type ProductionArtifacts struct {
	PublicKeys map[string]*rsa.PublicKey
	Profile    configprofile.Snapshot
	Modules    kernel.Config
	// Digests are SHA-256 values of the exact bytes decoded above. The map is
	// intentionally descriptive; the assembler must treat it as immutable.
	Digests          map[string]string
	GenerationDigest string
}

// ProductionLockInput contains only operator-supplied paths. No path is
// loaded from JSON lock content; manifests and artifacts must be explicitly
// mapped by the deployment host.
type ProductionLockInput struct {
	DeploymentFile string
	LockFile       string
	RetentionFile  string
	Manifests      map[buildlock.Key]string
	Artifacts      map[buildlock.Key]string
}

type ProductionLockedArtifacts struct {
	Artifacts ProductionArtifacts
	Lock      buildlock.Report
}

type EnvLookup func(string) (string, bool)

func required(lookup EnvLookup, key string) (string, error) {
	if lookup == nil {
		return "", ErrProductionConfig
	}
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" || strings.IndexByte(value, 0) >= 0 {
		return "", ErrProductionConfig
	}
	return value, nil
}

func absoluteFile(value string) bool {
	return strings.HasPrefix(value, "/") || len(value) > 2 && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

// LoadProductionSettings validates the deployment boundary without opening a
// database or reading credentials. The assembler remains responsible for
// loading the public-key set, profile, module catalogue and DB pool.
func LoadProductionSettings(lookup EnvLookup) (ProductionSettings, error) {
	var out ProductionSettings
	var err error
	if out.ListenAddr, err = required(lookup, "MEMORYS_LISTEN_ADDR"); err != nil {
		return ProductionSettings{}, err
	}
	if out.DatabaseURL, err = required(lookup, "MEMORYS_DATABASE_URL"); err != nil {
		return ProductionSettings{}, err
	}
	if out.Issuer, err = required(lookup, "MEMORYS_OAUTH_ISSUER"); err != nil {
		return ProductionSettings{}, err
	}
	if out.Audience, err = required(lookup, "MEMORYS_OAUTH_AUDIENCE"); err != nil {
		return ProductionSettings{}, err
	}
	for key, dst := range map[string]*string{
		"MEMORYS_OAUTH_JWKS_FILE":     &out.JWKSFile,
		"MEMORYS_PROFILE_FILE":        &out.ProfileFile,
		"MEMORYS_MODULE_CATALOG_FILE": &out.ModuleCatalogFile,
	} {
		if *dst, err = required(lookup, key); err != nil {
			return ProductionSettings{}, ErrProductionConfig
		}
	}
	if err := validateProductionSettings(out); err != nil {
		return ProductionSettings{}, err
	}
	return out, nil
}

func validateProductionSettings(s ProductionSettings) error {
	if s.ListenAddr == "" || s.DatabaseURL == "" || s.Issuer == "" || s.Audience == "" || s.JWKSFile == "" || s.ProfileFile == "" || s.ModuleCatalogFile == "" {
		return ErrProductionConfig
	}
	db, err := url.Parse(s.DatabaseURL)
	if err != nil || (db.Scheme != "postgres" && db.Scheme != "postgresql") || db.Host == "" || strings.EqualFold(db.Query().Get("sslmode"), "disable") {
		return ErrProductionConfig
	}
	issuer, err := url.Parse(s.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return ErrProductionConfig
	}
	if !absoluteFile(s.JWKSFile) || !absoluteFile(s.ProfileFile) || !absoluteFile(s.ModuleCatalogFile) {
		return ErrProductionConfig
	}
	return nil
}

func LoadProductionSettingsFromEnv() (ProductionSettings, error) {
	return LoadProductionSettings(os.LookupEnv)
}

// LoadProductionArtifacts loads one coherent deployment generation. Each
// file is read exactly once, hashed from those bytes, and decoded from the
// same snapshot; the assembler must bind these immutable inputs to verified
// factories and current identity status.
func LoadProductionArtifacts(settings ProductionSettings) (ProductionArtifacts, error) {
	var out ProductionArtifacts
	if validateProductionSettings(settings) != nil {
		return out, ErrProductionConfig
	}
	jwks, jwksDigest, err := readArtifactSnapshot(settings.JWKSFile)
	if err != nil {
		return out, ErrProductionConfig
	}
	profileRaw, profileDigest, err := readArtifactSnapshot(settings.ProfileFile)
	if err != nil {
		return out, ErrProductionConfig
	}
	modulesRaw, modulesDigest, err := readArtifactSnapshot(settings.ModuleCatalogFile)
	if err != nil {
		return out, ErrProductionConfig
	}
	keys, err := identitytoken.ParseRS256PublicKeys(jwks)
	if err != nil {
		return out, ErrProductionConfig
	}
	profile, err := configprofile.Decode(profileRaw)
	if err != nil {
		return out, ErrProductionConfig
	}
	modules, err := kernel.DecodeConfigFile(modulesRaw)
	if err != nil {
		return out, ErrProductionConfig
	}
	digests := map[string]string{"jwks": jwksDigest, "profile": profileDigest, "modules": modulesDigest}
	return ProductionArtifacts{
		PublicKeys:       keys,
		Profile:          profile,
		Modules:          modules,
		Digests:          digests,
		GenerationDigest: productionGenerationDigest(digests),
	}, nil
}

func productionGenerationDigest(digests map[string]string) string {
	material := "akos.production-artifacts/1\x00jwks\x00" + digests["jwks"] + "\x00modules\x00" + digests["modules"] + "\x00profile\x00" + digests["profile"]
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

// LoadProductionLockedArtifacts verifies the operator-supplied BuildLock
// inputs before loading the runtime artifacts. The lock verifier never
// executes artifacts or follows paths from JSON; the assembler receives the
// offline report separately and must still perform runtime health/IAM checks.
func LoadProductionLockedArtifacts(settings ProductionSettings, input ProductionLockInput) (ProductionLockedArtifacts, error) {
	var out ProductionLockedArtifacts
	if validateProductionSettings(settings) != nil {
		return out, ErrProductionConfig
	}
	lockReport, err := buildlock.VerifyFiles(input.DeploymentFile, input.LockFile, input.RetentionFile, input.Manifests, input.Artifacts)
	if err != nil {
		return out, ErrProductionConfig
	}
	artifacts, err := LoadProductionArtifacts(settings)
	if err != nil {
		return out, err
	}
	moduleKeys := make([]buildlock.Key, 0, len(artifacts.Modules.Modules))
	for _, module := range artifacts.Modules.Modules {
		moduleKeys = append(moduleKeys, buildlock.Key{Provider: module.Provider, Version: module.ImplementationVersion})
	}
	if !lockReport.CoversRuntimeModules(moduleKeys) {
		return out, ErrProductionConfig
	}
	return ProductionLockedArtifacts{Artifacts: artifacts, Lock: lockReport}, nil
}

func readArtifactSnapshot(path string) ([]byte, string, error) {
	if path == "" {
		return nil, "", ErrProductionConfig
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return nil, "", ErrProductionConfig
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

// NewProductionAuthenticator binds a verified artifact key set to the
// deployment issuer/audience while leaving current token status in the host's
// trusted database-backed StatusSource. It deliberately requires the host to
// state subject type and lifetime instead of inventing permissive defaults.
func NewProductionAuthenticator(settings ProductionSettings, artifacts ProductionArtifacts, subjectType string, maxLifetime time.Duration, status identitytoken.StatusSource) (*identitytoken.Authenticator, error) {
	if validateProductionSettings(settings) != nil || len(artifacts.PublicKeys) == 0 || status == nil {
		return nil, ErrProductionConfig
	}
	authenticator, err := identitytoken.New(identitytoken.Config{
		Issuer:      settings.Issuer,
		Audience:    settings.Audience,
		SubjectType: subjectType,
		Keys:        artifacts.PublicKeys,
		MaxLifetime: maxLifetime,
	}, status)
	if err != nil {
		return nil, ErrProductionConfig
	}
	return authenticator, nil
}

// ProductionAssembler is the host boundary for real DB pools, identity
// public keys, sealed profile/module snapshots and event consumers. It is
// deliberately replaceable; bootstrap never invents fixture implementations.
type ProductionAssembler interface {
	Assemble(context.Context, ProductionSettings) (Config, *events.Runner, func() error, error)
}

// ProductionArtifactAssembler is the preferred production boundary when the
// host needs the verified, immutable deployment inputs.  It is a separate
// interface so existing embedders can migrate without changing the basic
// assembler contract; NewProductionRuntime selects it automatically.
type ProductionArtifactAssembler interface {
	AssembleWithArtifacts(context.Context, ProductionSettings, ProductionArtifacts) (Config, *events.Runner, func() error, error)
}

// ProductionRunnerGroupAssembler is the multi-consumer production boundary.
// It always receives the verified artifact snapshot so each named outbox
// consumer is assembled from the same deployment generation.
type ProductionRunnerGroupAssembler interface {
	AssembleWithRunnerGroup(context.Context, ProductionSettings, ProductionArtifacts) (Config, *events.RunnerGroup, func() error, error)
}

type ProductionLockedArtifactAssembler interface {
	AssembleWithLockedArtifacts(context.Context, ProductionSettings, ProductionLockedArtifacts) (Config, *events.Runner, func() error, error)
}

// ProductionHealthAssembler is required by the strict BuildLock constructor.
// The host owns the real DB/IAM/module clients and must check them after
// assembly, before any event consumer or HTTP traffic is started.
type ProductionHealthAssembler interface {
	CheckProductionHealth(context.Context, Config) error
}

type ProductionRuntime struct {
	Runtime  *Runtime
	close    func() error
	once     sync.Once
	closeErr error
}

func NewProductionRuntime(ctx context.Context, settings ProductionSettings, assembler ProductionAssembler) (*ProductionRuntime, error) {
	if ctx == nil || assembler == nil || validateProductionSettings(settings) != nil {
		return nil, ErrProductionConfig
	}
	var config Config
	var runner *events.Runner
	var closeFn func() error
	var err error
	if artifactAssembler, ok := assembler.(ProductionArtifactAssembler); ok {
		artifacts, loadErr := LoadProductionArtifacts(settings)
		if loadErr != nil {
			return nil, loadErr
		}
		config, runner, closeFn, err = artifactAssembler.AssembleWithArtifacts(ctx, settings, artifacts)
	} else {
		config, runner, closeFn, err = assembler.Assemble(ctx, settings)
	}
	if err != nil || closeFn == nil {
		return nil, ErrProductionConfig
	}
	runtime, err := NewRuntime(config, runner)
	if err != nil {
		_ = closeFn()
		return nil, err
	}
	if err = runtime.Start(ctx); err != nil {
		_ = closeFn()
		return nil, err
	}
	return &ProductionRuntime{Runtime: runtime, close: closeFn}, nil
}

// NewProductionRuntimeWithRunnerGroup is the production constructor for
// deployments with more than one durable event consumer.
func NewProductionRuntimeWithRunnerGroup(ctx context.Context, settings ProductionSettings, assembler ProductionRunnerGroupAssembler) (*ProductionRuntime, error) {
	if ctx == nil || assembler == nil || validateProductionSettings(settings) != nil {
		return nil, ErrProductionConfig
	}
	artifacts, err := LoadProductionArtifacts(settings)
	if err != nil {
		return nil, err
	}
	config, group, closeFn, err := assembler.AssembleWithRunnerGroup(ctx, settings, artifacts)
	if err != nil || group == nil || closeFn == nil {
		return nil, ErrProductionConfig
	}
	runtime, err := NewRuntimeWithRunnerGroup(config, group)
	if err != nil {
		_ = closeFn()
		return nil, err
	}
	if err = runtime.Start(ctx); err != nil {
		_ = closeFn()
		return nil, err
	}
	return &ProductionRuntime{Runtime: runtime, close: closeFn}, nil
}

// NewProductionRuntimeWithBuildLock is the strict production constructor for
// a single event consumer. It does not invoke the assembler until all lock
// inputs have passed offline verification.
func NewProductionRuntimeWithBuildLock(ctx context.Context, settings ProductionSettings, input ProductionLockInput, assembler ProductionLockedArtifactAssembler) (*ProductionRuntime, error) {
	if ctx == nil || assembler == nil || validateProductionSettings(settings) != nil {
		return nil, ErrProductionConfig
	}
	locked, err := LoadProductionLockedArtifacts(settings, input)
	if err != nil {
		return nil, err
	}
	config, runner, closeFn, err := assembler.AssembleWithLockedArtifacts(ctx, settings, locked)
	if err != nil || closeFn == nil {
		return nil, ErrProductionConfig
	}
	health, ok := assembler.(ProductionHealthAssembler)
	if !ok || health.CheckProductionHealth(ctx, config) != nil {
		_ = closeFn()
		return nil, ErrProductionConfig
	}
	runtime, err := NewRuntime(config, runner)
	if err != nil {
		_ = closeFn()
		return nil, err
	}
	if err = runtime.Start(ctx); err != nil {
		_ = closeFn()
		return nil, err
	}
	return &ProductionRuntime{Runtime: runtime, close: closeFn}, nil
}

func (r *ProductionRuntime) Stop() error {
	if r == nil || r.Runtime == nil || r.close == nil {
		return ErrBootstrap
	}
	runtimeErr := r.Runtime.Stop()
	r.once.Do(func() { r.closeErr = r.close() })
	return errors.Join(runtimeErr, r.closeErr)
}
