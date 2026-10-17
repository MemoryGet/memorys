package configprofile_test

import (
	"bytes"
	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"testing"
)

func example(t *testing.T) configprofile.Snapshot {
	t.Helper()
	retriever := contracts.ModuleBinding{InstanceID: "keyword-reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}
	writer := contracts.ModuleBinding{InstanceID: "keyword-writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}
	s, err := configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1", Pipeline: contracts.PipelineProfile{ID: "pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "query", Version: 1}}, Index: contracts.IndexProfile{ID: "index", Version: 1, Mode: "independent", Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: writer}}, Query: contracts.QueryProfile{ID: "query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: retriever}, Routes: []contracts.RetrievalRoute{{ID: "keyword", Binding: retriever, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 10, Required: true}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 10, AnswerEvidenceLimit: 5, FusionMethod: "rrf", RankConstant: 60, RetrievalTimeoutMS: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestSnapshotTransitiveCommitmentsAndOwnership(t *testing.T) {
	s := example(t)
	owned, err := configprofile.Seal(s)
	if err != nil {
		t.Fatal(err)
	}
	s.Query.Routes[0].CandidateLimit = 9
	if owned.Query.Routes[0].CandidateLimit != 10 {
		t.Fatal("caller mutation reached owned snapshot")
	}
	if _, err := configprofile.Encode(s); err == nil {
		t.Fatal("modified content accepted old digest")
	}
	changed, err := configprofile.Seal(s)
	if err != nil || changed.Pipeline.Digest == owned.Pipeline.Digest || changed.Query.Digest == owned.Query.Digest || changed.Index.Digest != owned.Index.Digest {
		t.Fatal("query commitment did not propagate", err)
	}
	changed.Index.Mode = "composite"
	next, err := configprofile.Seal(changed)
	if err != nil || next.Pipeline.Digest == changed.Pipeline.Digest {
		t.Fatal("index commitment did not propagate", err)
	}
}
func TestSnapshotStrictDecodingAndReferenceValidation(t *testing.T) {
	s := example(t)
	raw, err := configprofile.Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := configprofile.Decode(raw)
	if err != nil || decoded.Pipeline.Digest != s.Pipeline.Digest {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(raw, []byte(" {}")...), bytes.Replace(raw, []byte(`"Format":`), []byte(`"unknown":`), 1), bytes.Replace(raw, []byte(`"Pipeline":`), []byte(`"pipeline":`), 1), bytes.Replace(raw, []byte(`"RankConstant":60`), []byte(`"RankConstant":61`), 1), []byte(`{"Format":"x","Format":"y"}`)} {
		if _, err := configprofile.Decode(bad); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
	s.Pipeline.QueryProfile.Version++
	if _, err := configprofile.Seal(s); err == nil {
		t.Fatal("inconsistent reference accepted")
	}
}
func TestSnapshotRejectsInvalidQueryBudgetsAndBindings(t *testing.T) {
	for _, change := range []func(*configprofile.Snapshot){func(s *configprofile.Snapshot) { s.Query.Routes[0].CandidateLimit = 11 }, func(s *configprofile.Snapshot) {
		s.Query.Reflection.Enabled = true
		s.Query.Reflection.MaxExtraRounds = 3
	}, func(s *configprofile.Snapshot) { s.Query.Policy.EnableGeneration = true }, func(s *configprofile.Snapshot) { s.Query.Routes[0].Binding.ContractVersion = "bad" }, func(s *configprofile.Snapshot) {
		s.Query.Routes[0].Channels = []contracts.Channel{contracts.KeywordChannel, contracts.KeywordChannel}
	}} {
		s := example(t)
		change(&s)
		if _, err := configprofile.Seal(s); err == nil {
			t.Fatal("invalid budget/binding accepted")
		}
	}
}
