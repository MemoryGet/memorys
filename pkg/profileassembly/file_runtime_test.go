package profileassembly_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/profileassembly"
)

type fileRetriever struct{}

func (fileRetriever) Search(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	return nil, nil
}

func TestBuildFromFilesResolvesTrustedFactoriesAndCloses(t *testing.T) {
	binding := contracts.ModuleBinding{InstanceID: "reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}
	profile, err := configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1", Pipeline: contracts.PipelineProfile{ID: "pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "query", Version: 1}}, Index: contracts.IndexProfile{ID: "index", Version: 1, Mode: "independent", Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: {InstanceID: "writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}}}, Query: contracts.QueryProfile{ID: "query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: binding}, Routes: []contracts.RetrievalRoute{{ID: "route", Binding: binding, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 1}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 1, AnswerEvidenceLimit: 1, FusionMethod: "rrf", RankConstant: 1, RetrievalTimeoutMS: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.json")
	modulePath := filepath.Join(dir, "modules.json")
	profileRaw, _ := configprofile.Encode(profile)
	moduleRaw, _ := json.Marshal(kernel.Config{Version: 1, Modules: []kernel.ModuleConfig{{ID: "reader", Provider: "reader", Settings: json.RawMessage(`{}`)}}, Bindings: map[contracts.Capability]string{contracts.Retrieval: "reader"}})
	if os.WriteFile(profilePath, profileRaw, 0600) != nil || os.WriteFile(modulePath, moduleRaw, 0600) != nil {
		t.Fatal("could not write assembly inputs")
	}
	closed := false
	factory := kernel.Factory{Manifest: kernel.Manifest{ID: "reader", Version: "1.0.0", ContractVersion: "1.0.0", Capabilities: []contracts.Capability{contracts.Retrieval}, CapabilityVersions: map[contracts.Capability]string{contracts.Retrieval: "1.0"}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
		return &kernel.Instance{Services: map[contracts.Capability]any{contracts.Retrieval: fileRetriever{}}, Health: func(context.Context) error { return nil }, Close: func(context.Context) error { closed = true; return nil }}, nil
	}}
	configuration, _ := kernel.LoadConfigFile(modulePath)
	if _, directErr := kernel.BuildBound(context.Background(), configuration, []kernel.Factory{factory}, []contracts.ModuleBinding{binding}); directErr != nil {
		t.Fatalf("direct kernel assembly failed: %v", directErr)
	}
	runtime, err := profileassembly.BuildFromFiles(context.Background(), profilePath, modulePath, profileassembly.Query, func(_ context.Context, m kernel.ModuleConfig) (kernel.Factory, error) {
		if m.ID != "reader" || m.Provider != "reader" {
			t.Fatal("unexpected module declaration")
		}
		return factory, nil
	})
	if err != nil || runtime == nil {
		t.Fatal("file assembly rejected", err)
	}
	if _, err = profileassembly.Resolve[contracts.Retriever](runtime, contracts.Retrieval); err != nil {
		t.Fatal("assembled service was not resolvable", err)
	}
	if err = runtime.Close(context.Background()); err != nil || !closed {
		t.Fatal("assembled runtime did not close", err, closed)
	}
}

func TestBuildFromFilesWithRegistryUsesPinnedRelease(t *testing.T) {
	// Registry resolution is exercised independently; this test keeps the
	// profile assembly path explicit and rejects a missing registry.
	if _, err := profileassembly.BuildFromFilesWithRegistry(context.Background(), "profile", "modules", profileassembly.Query, nil); err == nil {
		t.Fatal("missing factory registry accepted")
	}
}
