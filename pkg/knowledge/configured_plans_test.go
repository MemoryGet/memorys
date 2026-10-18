package knowledge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type configuredAuth struct{}

func (configuredAuth) Authorize(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return contracts.AccessScope{}, nil
}

type configuredFacts struct{}

func (configuredFacts) LoadCurrentFacts(context.Context, contracts.RequestContext, contracts.AccessScope) (queryread.ControlFacts, error) {
	return queryread.ControlFacts{}, nil
}

type configuredReads struct{}

func (configuredReads) WithinRead(context.Context, contracts.AccessScope, func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	return nil
}

func configuredProfile(t *testing.T) configprofile.Snapshot {
	t.Helper()
	binding := contracts.ModuleBinding{InstanceID: "reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}
	p, e := configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1", Pipeline: contracts.PipelineProfile{ID: "pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "query", Version: 1}}, Index: contracts.IndexProfile{ID: "index", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: {InstanceID: "writer", Capability: contracts.IndexWriting, ContractVersion: "1.0"}}}, Query: contracts.QueryProfile{ID: "query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: binding}, Routes: []contracts.RetrievalRoute{{ID: "route", Binding: binding, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 10}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 10, AnswerEvidenceLimit: 5, FusionMethod: "rrf", RankConstant: 60, RetrievalTimeoutMS: 1000}}})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestConfiguredPlansRejectsIncompleteDependenciesAndCloses(t *testing.T) {
	p := configuredProfile(t)
	var nilAuth *configuredAuth
	if _, e := NewConfiguredPlans(ConfiguredPlansConfig{Profile: p, Authorizer: nilAuth, Facts: configuredFacts{}, Reads: configuredReads{}, Limits: queryread.Limits{PageSize: 1, MaxPages: 1, MaxFacts: 1, MaxGenerations: 1}, Resolve: func(context.Context, contracts.ModuleBinding) (contracts.Retriever, error) { return nil, nil }}); e == nil {
		t.Fatal("typed nil authorizer accepted")
	}
	plans, e := NewConfiguredPlans(ConfiguredPlansConfig{Profile: p, Authorizer: configuredAuth{}, Facts: configuredFacts{}, Reads: configuredReads{}, Limits: queryread.Limits{PageSize: 1, MaxPages: 1, MaxFacts: 1, MaxGenerations: 1}, Resolve: func(context.Context, contracts.ModuleBinding) (contracts.Retriever, error) { return nil, nil }})
	if e != nil {
		t.Fatal(e)
	}
	plans.Close()
	if _, e = plans.Prepare(context.Background(), contracts.Principal{}, contracts.RetrieveRequest{}); e == nil {
		t.Fatal("closed configured plans accepted prepare")
	}
}
func TestParentInventoryDigestBindsPolicyAndEpochWithoutFloatingNumbers(t *testing.T) {
	q := contracts.RetrieveRequest{Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "actor", Action: "knowledge.search", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}}
	in := plannedInventory{identity: contracts.QueryReadIdentity{StoreEpochID: "store", ReadVersion: "read", Watermark: "water"}, epoch: contracts.BackendEpoch{ID: "epoch", KnowledgeBaseID: "kb", Number: 1 << 63, ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), State: "active"}}
	first, e := parentInventoryDigest(q, in)
	if e != nil || len(first) != 64 {
		t.Fatal(e, first)
	}
	in.epoch.Number++
	second, e := parentInventoryDigest(q, in)
	if e != nil || first == second {
		t.Fatal("uint64 epoch change did not change parent digest")
	}
	q.Scope.PolicyVersion = 2
	third, e := parentInventoryDigest(q, in)
	if e != nil || second == third {
		t.Fatal("policy change did not change parent digest")
	}
}
