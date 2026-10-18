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
	Digests map[string]string
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
	return ProductionArtifacts{
		PublicKeys: keys,
		Profile:    profile,
		Modules:    modules,
		Digests:    map[string]string{"jwks": jwksDigest, "profile": profileDigest, "modules": modulesDigest},
	}, nil
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

func (r *ProductionRuntime) Stop() error {
	if r == nil || r.Runtime == nil || r.close == nil {
		return ErrBootstrap
	}
	runtimeErr := r.Runtime.Stop()
	r.once.Do(func() { r.closeErr = r.close() })
	return errors.Join(runtimeErr, r.closeErr)
}
