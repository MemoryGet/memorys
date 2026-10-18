package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/events"
	"github.com/MemoryGet/memorys/pkg/identitytoken"
	"github.com/MemoryGet/memorys/pkg/kernel"
	jose "github.com/go-jose/go-jose/v4"
)

func productionEnv(overrides map[string]string) EnvLookup {
	base := map[string]string{
		"MEMORYS_LISTEN_ADDR":         "127.0.0.1:8080",
		"MEMORYS_DATABASE_URL":        "postgres://memorys:secret@db.example/knowledge?sslmode=verify-full",
		"MEMORYS_OAUTH_ISSUER":        "https://issuer.example",
		"MEMORYS_OAUTH_AUDIENCE":      "knowledge-api",
		"MEMORYS_OAUTH_JWKS_FILE":     "C:/run/secrets/jwks.json",
		"MEMORYS_PROFILE_FILE":        "C:/run/config/profile.json",
		"MEMORYS_MODULE_CATALOG_FILE": "C:/run/config/modules.json",
	}
	for key, value := range overrides {
		if value == "" {
			delete(base, key)
		} else {
			base[key] = value
		}
	}
	return func(key string) (string, bool) { value, ok := base[key]; return value, ok }
}

func TestLoadProductionSettingsRejectsUnsafeOrIncompleteDeployment(t *testing.T) {
	if _, err := LoadProductionSettings(productionEnv(map[string]string{"MEMORYS_DATABASE_URL": "postgres://db/knowledge?sslmode=disable"})); !errors.Is(err, ErrProductionConfig) {
		t.Fatal("disabled TLS database accepted", err)
	}
	if _, err := LoadProductionSettings(productionEnv(map[string]string{"MEMORYS_OAUTH_ISSUER": "http://issuer.example"})); !errors.Is(err, ErrProductionConfig) {
		t.Fatal("non-TLS issuer accepted", err)
	}
	if _, err := LoadProductionSettings(productionEnv(map[string]string{"MEMORYS_PROFILE_FILE": "relative/profile.json"})); !errors.Is(err, ErrProductionConfig) {
		t.Fatal("relative profile path accepted", err)
	}
	if _, err := LoadProductionSettings(productionEnv(map[string]string{"MEMORYS_MODULE_CATALOG_FILE": ""})); !errors.Is(err, ErrProductionConfig) {
		t.Fatal("missing module catalog accepted", err)
	}
}

type productionAssembler struct {
	called bool
	closed bool
	closes int
}

func (a *productionAssembler) Assemble(_ context.Context, settings ProductionSettings) (Config, *events.Runner, func() error, error) {
	a.called = settings.Audience == "knowledge-api"
	return Config{}, nil, func() error { a.closed = true; a.closes++; return nil }, nil
}

func TestNewProductionRuntimeOwnsAssemblerLifecycle(t *testing.T) {
	a := &productionAssembler{}
	settings, err := LoadProductionSettings(productionEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewProductionRuntime(context.Background(), settings, a)
	if err != nil || runtime == nil || !a.called {
		t.Fatal("production runtime did not assemble", err)
	}
	if err = runtime.Stop(); err != nil || !a.closed {
		t.Fatal("production runtime did not close dependencies", err, a.closed)
	}
	if err = runtime.Stop(); err != nil || a.closes != 1 {
		t.Fatal("production runtime stop was not idempotent", err)
	}
}

func TestNewProductionRuntimeRejectsMissingAssembler(t *testing.T) {
	settings, err := LoadProductionSettings(productionEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewProductionRuntime(context.Background(), settings, nil); !errors.Is(err, ErrProductionConfig) {
		t.Fatal("missing production assembler accepted", err)
	}
}

func TestLoadProductionArtifactsFailsClosedForMissingDeploymentFiles(t *testing.T) {
	settings, err := LoadProductionSettings(productionEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = LoadProductionArtifacts(settings); !errors.Is(err, ErrProductionConfig) {
		t.Fatal("missing deployment artifacts accepted", err)
	}
}

type artifactAssembler struct {
	called  bool
	keys    int
	digests map[string]string
}

type lockedArtifactAssembler struct{ called bool }

func (a *lockedArtifactAssembler) AssembleWithLockedArtifacts(_ context.Context, _ ProductionSettings, _ ProductionLockedArtifacts) (Config, *events.Runner, func() error, error) {
	a.called = true
	return Config{}, nil, func() error { return nil }, nil
}

type productionTokenStatus struct{}

func (productionTokenStatus) Active(context.Context, identitytoken.StatusQuery) (bool, error) {
	return true, nil
}

func (a *artifactAssembler) Assemble(_ context.Context, _ ProductionSettings) (Config, *events.Runner, func() error, error) {
	return Config{}, nil, func() error { return nil }, nil
}

func (a *artifactAssembler) AssembleWithArtifacts(_ context.Context, _ ProductionSettings, artifacts ProductionArtifacts) (Config, *events.Runner, func() error, error) {
	a.called = true
	a.keys = len(artifacts.PublicKeys)
	a.digests = artifacts.Digests
	return Config{}, nil, func() error { return nil }, nil
}

func TestNewProductionRuntimePassesVerifiedArtifactsToAwareAssembler(t *testing.T) {
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "key-1", Algorithm: string(jose.RS256), Use: "sig"}}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := configprofile.Seal(configprofile.Snapshot{
		Format:   "akos.profile-snapshot/1",
		Pipeline: contracts.PipelineProfile{ID: "pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "query", Version: 1}},
		Index:    contracts.IndexProfile{ID: "index", Version: 1, Mode: "independent", Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: {InstanceID: "writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}}},
		Query:    contracts.QueryProfile{ID: "query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: {InstanceID: "reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}}, Routes: []contracts.RetrievalRoute{{ID: "route", Binding: contracts.ModuleBinding{InstanceID: "reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 1}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 1, AnswerEvidenceLimit: 1, FusionMethod: "rrf", RankConstant: 1, RetrievalTimeoutMS: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	profileRaw, err := configprofile.Encode(profile)
	if err != nil {
		t.Fatal(err)
	}
	moduleRaw, err := json.Marshal(kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "reader", Provider: "reader", Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.Retrieval: "reader"}})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string][]byte{"jwks.json": jwks, "profile.json": profileRaw, "modules.json": moduleRaw}
	for name, raw := range paths {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := LoadProductionSettings(productionEnv(map[string]string{
		"MEMORYS_OAUTH_JWKS_FILE":     filepath.Join(dir, "jwks.json"),
		"MEMORYS_PROFILE_FILE":        filepath.Join(dir, "profile.json"),
		"MEMORYS_MODULE_CATALOG_FILE": filepath.Join(dir, "modules.json"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	a := &artifactAssembler{}
	runtime, err := NewProductionRuntime(context.Background(), settings, a)
	if err != nil || runtime == nil || !a.called || a.keys != 1 || len(a.digests) != 3 || a.digests["jwks"] == "" || a.digests["profile"] == "" || a.digests["modules"] == "" {
		t.Fatal("verified artifacts were not passed to aware assembler", err, a.called, a.keys, a.digests)
	}
	artifacts, err := LoadProductionArtifacts(settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewProductionAuthenticator(settings, artifacts, "service", time.Minute, productionTokenStatus{}); err != nil {
		t.Fatal("verified production authenticator rejected", err)
	}
	if err := runtime.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestNewProductionRuntimeWithBuildLockRejectsBeforeAssembler(t *testing.T) {
	settings, err := LoadProductionSettings(productionEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	a := &lockedArtifactAssembler{}
	if _, err = NewProductionRuntimeWithBuildLock(context.Background(), settings, ProductionLockInput{}, a); !errors.Is(err, ErrProductionConfig) {
		t.Fatal("missing BuildLock inputs accepted", err)
	}
	if a.called {
		t.Fatal("assembler invoked before BuildLock verification")
	}
}
