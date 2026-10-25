package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func stateCopyFixture(t testing.TB) contracts.CurrentDocumentState {
	t.Helper()
	revision, set, generation := contracts.ID("revision"), contracts.ID("set"), contracts.ID("generation")
	digest := contracts.Digest("digest")
	deleted := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s := contracts.CurrentDocumentState{
		Document: contracts.Document{ID: "doc", ActiveRevisionID: &revision, ActiveGenerationSetID: &set, DeletedAt: &deleted},
		GenerationSet: &contracts.GenerationSet{ID: set, Channels: map[contracts.Channel]contracts.IndexGeneration{
			contracts.VectorChannel: {ID: generation, EmbeddingSpec: &contracts.EmbeddingSpec{Dimension: 768},
				BackendBinding: &contracts.BackendBinding{GenerationID: &generation},
				Readiness:      &contracts.ReadinessProof{Generations: []contracts.GenerationReadiness{{GenerationID: generation, ChunkCount: 1, EmbeddingSpecDigest: &digest}}}},
		}},
		PublicationReview: &contracts.CurrentPublicationReview{Approval: contracts.Approval{ID: "approval", DecidedAt: deleted}},
	}
	// Match production's input seam: admitted values were freshly JSON-decoded
	// by prefetch, never arbitrary caller-owned Go values with monotonic clocks.
	raw, err := json.Marshal(s)
	if err != nil || json.Unmarshal(raw, &s) != nil {
		t.Fatal("state fixture decode", err)
	}
	return s
}

func TestQueryStateCopyOwnsEveryNestedReference(t *testing.T) {
	state := stateCopyFixture(t)
	r := &queryRepository{states: map[contracts.ID]contracts.CurrentDocumentState{}}
	if err := r.takeState("doc", state); err != nil {
		t.Fatal(err)
	}
	want, err := copyQuery(state)
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.load(context.Background(), "doc")
	if err != nil || !reflect.DeepEqual(first, want) {
		t.Fatal("copy changed decoded value", err)
	}
	*first.Document.ActiveRevisionID = "changed"
	*first.Document.ActiveGenerationSetID = "changed"
	*first.Document.DeletedAt = first.Document.DeletedAt.Add(time.Hour)
	first.GenerationSet.ID = "changed"
	g := first.GenerationSet.Channels[contracts.VectorChannel]
	g.EmbeddingSpec.Dimension = 1
	*g.BackendBinding.GenerationID = "changed"
	g.BackendBinding.BackendID = "changed"
	g.Readiness.ContentDigest = "changed"
	*g.Readiness.Generations[0].EmbeddingSpecDigest = "changed"
	g.Readiness.Generations[0].ChunkCount = 99
	delete(first.GenerationSet.Channels, contracts.VectorChannel)
	first.PublicationReview.Revision.ContentHash = "changed"
	first.PublicationReview.Approval.ID = "changed"
	second, err := r.load(context.Background(), "doc")
	if err != nil || !reflect.DeepEqual(second, want) {
		t.Fatal("returned state poisoned immutable read-view metadata", err)
	}
}

func TestQueryStateCopyAllocationBudget(t *testing.T) {
	s := stateCopyFixture(t)
	r := &queryRepository{states: map[contracts.ID]contracts.CurrentDocumentState{}}
	if err := r.takeState("doc", s); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(10, func() {
		if _, err := r.load(context.Background(), "doc"); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("admitted read-view state copy allocations: %.0f", allocations)
	if allocations > 20 {
		t.Fatalf("read-view state copy budget exceeded: %.0f > 20", allocations)
	}
}

func BenchmarkQueryStateCopy(b *testing.B) {
	s := stateCopyFixture(b)
	r := &queryRepository{states: map[contracts.ID]contracts.CurrentDocumentState{"doc": s}}
	for _, impl := range []struct {
		name string
		run  func() (contracts.CurrentDocumentState, error)
	}{{"json_round_trip", func() (contracts.CurrentDocumentState, error) { return copyQuery(s) }},
		{"admitted_state", func() (contracts.CurrentDocumentState, error) { return r.load(context.Background(), "doc") }}} {
		b.Run(impl.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := impl.run(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestQueryStateCopyPreservesOptionalValues(t *testing.T) {
	full := stateCopyFixture(t)
	for _, state := range []contracts.CurrentDocumentState{{},
		{GenerationSet: &contracts.GenerationSet{}},
		{GenerationSet: &contracts.GenerationSet{Channels: map[contracts.Channel]contracts.IndexGeneration{}}},
		{GenerationSet: &contracts.GenerationSet{Channels: map[contracts.Channel]contracts.IndexGeneration{
			contracts.KeywordChannel: {Readiness: &contracts.ReadinessProof{}},
			contracts.VectorChannel:  {Readiness: &contracts.ReadinessProof{Generations: []contracts.GenerationReadiness{}}},
		}}}, full} {
		r := &queryRepository{states: map[contracts.ID]contracts.CurrentDocumentState{}}
		if err := r.takeState("doc", state); err != nil {
			t.Fatal(err)
		}
		want, err := copyQuery(state)
		if err != nil {
			t.Fatal(err)
		}
		got, err := r.load(context.Background(), "doc")
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("optional state changed", err)
		}
	}
}
