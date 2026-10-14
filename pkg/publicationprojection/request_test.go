package publicationprojection_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
	"github.com/MemoryGet/memorys/pkg/publicationprojection"
)

func requestFixture(t *testing.T) ([]byte, publicationprojection.CallIdentity, contracts.RetrieveRequest) {
	t.Helper()
	raw, err := os.ReadFile("../../docs/architecture/examples/publication-projection-golden.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			Input string `json:"input_json"`
		}
	}
	if json.Unmarshal(raw, &golden) != nil {
		t.Fatal("invalid golden")
	}
	raw = []byte(golden.Cases[0].Input)
	r, digest, err := publicationprojection.ValidatedDigest(raw)
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Unix(0, r.ExpiresAtUnixNS)
	q := contracts.RetrieveRequest{Request: contracts.RequestContext{RequestID: r.RequestID, ProfileDigest: r.PipelineDigest, Deadline: exp}, Scope: contracts.AccessScope{ScopeID: r.ChildScopeID, SubjectID: "user", Action: "knowledge.search", SpaceID: r.SpaceID, KnowledgeBaseIDs: []contracts.ID{r.KnowledgeBaseID}, PolicyVersion: r.PolicyVersion, ExpiresAt: exp}, Query: contracts.Query{Text: "中文故障"}, Publication: contracts.PublicationFilter{SpaceID: r.SpaceID, KnowledgeBaseIDs: []contracts.ID{r.KnowledgeBaseID}, RevisionIDs: []contracts.ID{"rev"}, GenerationIDs: []contracts.ID{"keyword-gen", "vector-gen"}, SnapshotDigest: digest}, BackendEpochID: r.BackendEpochID, RouteID: r.RouteID, Channels: r.Channels, Limit: 2}
	spec := contracts.EmbeddingSpec{ProviderModel: "fixture", ModelRevision: "1", Dimension: 2, Distance: "cosine", Normalization: "l2", PreprocessVersion: "1"}
	q.Query.Vector = contracts.Vector{0.1, 0.2}
	q.Query.EmbeddingSpec = &spec
	for _, entry := range r.Entries {
		id := entry.GenerationID
		g := contracts.IndexGeneration{ID: id, KnowledgeBaseID: r.KnowledgeBaseID, RevisionID: entry.RevisionID, Channel: entry.Channel, ProfileDigest: entry.IndexProfileDigest, BackendEpochID: r.BackendEpochID, State: "ready", BackendBinding: &contracts.BackendBinding{PlatformResourceType: "generation", PlatformResourceID: id, ModuleInstanceID: "writer", BackendID: "private", GenerationID: &id, Purpose: "retrieval"}, Readiness: &contracts.ReadinessProof{GenerationSetID: entry.GenerationSetID, AttemptID: "attempt", ContentDigest: contracts.Digest(strings.Repeat("c", 64)), ProfileDigest: entry.IndexProfileDigest, Generations: []contracts.GenerationReadiness{{GenerationID: id, Channel: entry.Channel, BackendEpochID: r.BackendEpochID, ChunkCount: 1}}}}
		if entry.Channel == contracts.VectorChannel {
			g.EmbeddingSpec = &spec
			d := contracts.Digest(strings.Repeat("d", 64))
			g.Readiness.Generations[0].EmbeddingSpecDigest = &d
		}
		q.Generations = append(q.Generations, g)
	}
	return raw, publicationprojection.CallIdentity{InvocationID: r.InvocationID, ModuleInstanceID: r.ModuleInstanceID, Limit: q.Limit}, q
}

func TestProjectionMatchesActualRetrieveWireRoundtrip(t *testing.T) {
	raw, expected, q := requestFixture(t)
	if err := publicationprojection.ValidateRequest(raw, expected, q); err != nil {
		t.Fatal(err)
	}
	call := modulewire.Call{InvocationID: expected.InvocationID, ModuleInstanceID: expected.ModuleInstanceID}
	wire, err := modulewire.EncodeRetrieveRequest(call, q)
	if err != nil {
		t.Fatal(err)
	}
	actualCall, actual, err := modulewire.DecodeRetrieveRequest(wire)
	if err != nil || actualCall != call {
		t.Fatal(err)
	}
	if err := publicationprojection.ValidateRequest(raw, expected, actual); err != nil {
		t.Fatal("wire lost projection bindings", err)
	}
}

func TestProjectionRejectsBroadenedAndMissingFilterObjects(t *testing.T) {
	for _, change := range []func(*contracts.RetrieveRequest){
		func(q *contracts.RetrieveRequest) {
			q.Scope.KnowledgeBaseIDs = append(q.Scope.KnowledgeBaseIDs, "other")
		},
		func(q *contracts.RetrieveRequest) { q.Publication.KnowledgeBaseIDs = []contracts.ID{"other"} },
		func(q *contracts.RetrieveRequest) {
			q.Publication.RevisionIDs = append(q.Publication.RevisionIDs, "old")
		},
		func(q *contracts.RetrieveRequest) { q.Publication.GenerationIDs = q.Publication.GenerationIDs[:1] },
		func(q *contracts.RetrieveRequest) { q.Generations[1] = q.Generations[0] },
		func(q *contracts.RetrieveRequest) { q.Generations[0].KnowledgeBaseID = "other" },
		func(q *contracts.RetrieveRequest) { q.Generations[0].BackendEpochID = "old" },
		func(q *contracts.RetrieveRequest) { q.Generations[0].Readiness.GenerationSetID = "wrong" },
		func(q *contracts.RetrieveRequest) {
			q.Generations[0].ProfileDigest = contracts.Digest(strings.Repeat("e", 64))
		},
		func(q *contracts.RetrieveRequest) { q.Channels = q.Channels[:1] },
	} {
		raw, e, q := requestFixture(t)
		change(&q)
		if err := publicationprojection.ValidateRequest(raw, e, q); err != publicationprojection.ErrInvalid {
			t.Fatal("bad filter accepted", err)
		}
	}
}

func TestProjectionRejectsIdentityBudgetAndExpiryMismatch(t *testing.T) {
	for _, change := range []func(*publicationprojection.CallIdentity, *contracts.RetrieveRequest){
		func(e *publicationprojection.CallIdentity, _ *contracts.RetrieveRequest) { e.InvocationID = "wrong" },
		func(e *publicationprojection.CallIdentity, _ *contracts.RetrieveRequest) {
			e.ModuleInstanceID = "wrong"
		},
		func(_ *publicationprojection.CallIdentity, q *contracts.RetrieveRequest) { q.Limit++ },
		func(_ *publicationprojection.CallIdentity, q *contracts.RetrieveRequest) { q.Scope.ScopeID = "parent" },
		func(_ *publicationprojection.CallIdentity, q *contracts.RetrieveRequest) { q.Scope.PolicyVersion++ },
		func(_ *publicationprojection.CallIdentity, q *contracts.RetrieveRequest) {
			q.Request.ProfileDigest = contracts.Digest(strings.Repeat("f", 64))
		},
		func(_ *publicationprojection.CallIdentity, q *contracts.RetrieveRequest) {
			q.Publication.SnapshotDigest = contracts.Digest(strings.Repeat("a", 64))
		},
		func(_ *publicationprojection.CallIdentity, q *contracts.RetrieveRequest) {
			q.Request.Deadline = q.Request.Deadline.Add(time.Nanosecond)
		},
		func(_ *publicationprojection.CallIdentity, q *contracts.RetrieveRequest) {
			q.Scope.ExpiresAt = q.Scope.ExpiresAt.Add(-time.Nanosecond)
		},
	} {
		raw, e, q := requestFixture(t)
		change(&e, &q)
		if err := publicationprojection.ValidateRequest(raw, e, q); err != publicationprojection.ErrInvalid {
			t.Fatal("identity/budget/expiry accepted", err)
		}
	}
}
