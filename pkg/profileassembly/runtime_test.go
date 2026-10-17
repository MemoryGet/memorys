package profileassembly_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/kernel"
	"github.com/MemoryGet/memorys/pkg/profileassembly"
)

type reader struct {
	name   string
	closed bool
}

func (r *reader) Search(context.Context, contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	if r.closed {
		return nil, errors.New("closed")
	}
	return nil, nil
}
func factory(id string, created *int) kernel.Factory {
	return kernel.Factory{Manifest: kernel.Manifest{ID: id, Version: "1.0.0", ContractVersion: "1.0", Capabilities: []contracts.Capability{contracts.Retrieval}}, Validate: func(json.RawMessage) error { return nil }, Create: func(context.Context, json.RawMessage, kernel.Lookup) (*kernel.Instance, error) {
		*created++
		r := &reader{name: id}
		return &kernel.Instance{Services: map[contracts.Capability]any{contracts.Retrieval: r}, Health: func(context.Context) error { return nil }, Close: func(context.Context) error { r.closed = true; return nil }}, nil
	}}
}
func snapshot(t *testing.T) configprofile.Snapshot {
	t.Helper()
	a := contracts.ModuleBinding{InstanceID: "primary", Capability: contracts.Retrieval, ContractVersion: "1.0"}
	b := a
	b.InstanceID = "secondary"
	s, err := configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1", Pipeline: contracts.PipelineProfile{ID: "pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "query", Version: 1}}, Index: contracts.IndexProfile{ID: "index", Version: 1, Mode: "independent", Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: {InstanceID: "writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}}}, Query: contracts.QueryProfile{ID: "query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: a}, Routes: []contracts.RetrievalRoute{{ID: "first", Binding: a, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 5}, {ID: "second", Binding: b, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 5}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 10, AnswerEvidenceLimit: 5, FusionMethod: "rrf", RankConstant: 60, RetrievalTimeoutMS: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func plan(provider string) kernel.Config {
	return kernel.Config{Version: 1, Bindings: map[contracts.Capability]string{contracts.Retrieval: "primary"}, Modules: []kernel.ModuleConfig{{ID: "primary", Provider: provider, Settings: json.RawMessage(`{}`)}, {ID: "secondary", Provider: "other", Settings: json.RawMessage(`{}`)}}}
}
func TestNamedRoutesAndProviderReplacement(t *testing.T) {
	s := snapshot(t)
	identities := map[string]bool{}
	for _, provider := range []string{"first", "replacement"} {
		created := 0
		rt, err := profileassembly.Build(context.Background(), s, profileassembly.Query, plan(provider), []kernel.Factory{factory(provider, &created), factory("other", &created)})
		if err != nil || created != 2 {
			t.Fatal(err, created)
		}
		a, err := profileassembly.ResolveRoute[contracts.Retriever](rt, "first")
		if err != nil || a.(*reader).name != provider {
			t.Fatal("wrong named route", err)
		}
		b, err := profileassembly.ResolveRoute[contracts.Retriever](rt, "second")
		if err != nil || b.(*reader).name != "other" {
			t.Fatal("default binding replaced named route", err)
		}
		own, _ := rt.Profile()
		own.Query.Routes[0].Binding.InstanceID = "wrong"
		again, _ := rt.Profile()
		if again.Query.Routes[0].Binding.InstanceID != "primary" {
			t.Fatal("profile ownership lost")
		}
		id, _ := rt.Identity()
		identities[id.ConfigVersion] = true
		if err := rt.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := profileassembly.ResolveRoute[contracts.Retriever](rt, "first"); err == nil {
			t.Fatal("closed runtime resolved")
		}
		if _, err := a.Search(context.Background(), contracts.RetrieveRequest{}); err == nil {
			t.Fatal("managed old reference still usable")
		}
	}
	if len(identities) != 2 {
		t.Fatal("provider replacement did not change assembly identity")
	}
}
func TestUnsupportedBindingsFailBeforeCreate(t *testing.T) {
	for _, change := range []func(*configprofile.Snapshot){func(s *configprofile.Snapshot) { s.Query.Routes[1].Binding.InstanceID = "missing" }, func(s *configprofile.Snapshot) { s.Query.Routes[1].Binding.ContractVersion = "2.0" }, func(s *configprofile.Snapshot) {
		s.Query.Routes[1].Binding.RequiredFeatures = []string{"missing.feature"}
	}} {
		s := snapshot(t)
		change(&s)
		s, err := configprofile.Seal(s)
		if err != nil {
			t.Fatal(err)
		}
		created := 0
		if _, err := profileassembly.Build(context.Background(), s, profileassembly.Query, plan("first"), []kernel.Factory{factory("first", &created), factory("other", &created)}); err == nil || created != 0 {
			t.Fatal("invalid binding created a module", err, created)
		}
	}
	created := 0
	if _, err := profileassembly.Build(context.Background(), snapshot(t), profileassembly.Index, plan("first"), []kernel.Factory{factory("first", &created), factory("other", &created)}); err == nil || created != 0 {
		t.Fatal("query deployment claimed index readiness")
	}
}
