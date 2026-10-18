package configprofile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestLoadFileRequiresSealedSingleSnapshot(t *testing.T) {
	s, err := configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1", Pipeline: contracts.PipelineProfile{ID: "pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "query", Version: 1}}, Index: contracts.IndexProfile{ID: "index", Version: 1, Mode: "independent", Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: {InstanceID: "writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}}}, Query: contracts.QueryProfile{ID: "query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: {InstanceID: "reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}}, Routes: []contracts.RetrievalRoute{{ID: "route", Binding: contracts.ModuleBinding{InstanceID: "reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 1}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 1, AnswerEvidenceLimit: 1, FusionMethod: "rrf", RankConstant: 1, RetrievalTimeoutMS: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	raw, err := configprofile.Encode(s)
	if err != nil || os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("could not write profile")
	}
	loaded, err := configprofile.LoadFile(path)
	if err != nil || loaded.Pipeline.Digest != s.Pipeline.Digest {
		t.Fatal("sealed profile did not load", err)
	}
	if os.WriteFile(path, append(raw, []byte(" {}")...), 0600) != nil {
		t.Fatal("could not write malformed profile")
	}
	if _, err = configprofile.LoadFile(path); err == nil {
		t.Fatal("trailing profile JSON accepted")
	}
}
