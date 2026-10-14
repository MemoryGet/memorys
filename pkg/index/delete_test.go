package index_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/index"
)

func deletion() contracts.IndexDeleteRequest {
	r := request()
	r.Scope.Action = "index.delete"
	return contracts.IndexDeleteRequest{Request: r.Request, Scope: r.Scope, GenerationID: "generation"}
}
func TestGenerationDeleteIsIdempotentAndBlocksLateProjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m := &index.MemoryIndex{AuthorizeDelete: func(context.Context, contracts.AccessScope, contracts.ID) error { return nil }}
	r := record("allowed")
	if err := m.Upsert(r); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := m.Delete(ctx, deletion()); err != nil {
			t.Fatal(err)
		}
	}
	results, err := m.Search(ctx, request())
	if err != nil || len(results) != 0 {
		t.Fatal("deleted projection visible", err)
	}
	if m.Upsert(r) == nil {
		t.Fatal("late job resurrected generation")
	}
}
func TestChunkDeletePreservesOtherChunksAndGenerationOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m := &index.MemoryIndex{AuthorizeDelete: func(context.Context, contracts.AccessScope, contracts.ID) error { return nil }}
	a, b := record("doc"), record("doc")
	a.Chunk.ID = "first"
	b.Chunk.ID = "second"
	if m.Upsert(a) != nil || m.Upsert(b) != nil {
		t.Fatal("projection setup")
	}
	d := deletion()
	d.ChunkIDs = []contracts.ID{"first"}
	if m.Delete(ctx, d) != nil || m.Delete(ctx, d) != nil {
		t.Fatal("chunk cleanup retry")
	}
	q := request()
	q.Limit = 10
	results, err := m.Search(ctx, q)
	if err != nil || len(results) != 1 || results[0].ChunkID != "second" {
		t.Fatal("unselected chunk removed", results, err)
	}
	if m.Upsert(a) == nil {
		t.Fatal("late chunk restored")
	}
	foreign := b
	foreign.Chunk.ID = "third"
	foreign.SpaceID = "other"
	if m.Upsert(foreign) == nil {
		t.Fatal("generation ownership changed")
	}
}
func TestDeleteRejectsMissingAuthorityCrossScopeAndCancelledRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m := &index.MemoryIndex{}
	if m.Upsert(record("allowed")) != nil {
		t.Fatal("setup")
	}
	if m.Delete(ctx, deletion()) == nil {
		t.Fatal("missing authority accepted")
	}
	m.AuthorizeDelete = func(context.Context, contracts.AccessScope, contracts.ID) error { return errors.New("revoked") }
	if m.Delete(ctx, deletion()) == nil {
		t.Fatal("denied delete accepted")
	}
	m.AuthorizeDelete = func(context.Context, contracts.AccessScope, contracts.ID) error { return nil }
	for _, change := range []func(*contracts.IndexDeleteRequest){func(r *contracts.IndexDeleteRequest) { r.Scope.SpaceID = "other" }, func(r *contracts.IndexDeleteRequest) { r.Scope.KnowledgeBaseIDs = []contracts.ID{"other"} }, func(r *contracts.IndexDeleteRequest) { r.GenerationID = "unknown" }, func(r *contracts.IndexDeleteRequest) { r.ChunkIDs = []contracts.ID{"first", "first"} }, func(r *contracts.IndexDeleteRequest) { r.Scope.Action = "knowledge.search" }} {
		d := deletion()
		change(&d)
		if m.Delete(ctx, d) == nil {
			t.Fatal("unsafe deletion")
		}
	}
	if results, err := m.Search(ctx, request()); err != nil || len(results) != 1 {
		t.Fatal("denied request mutated data")
	}
	cancel()
	if m.Delete(ctx, deletion()) != context.Canceled {
		t.Fatal("cancelled deletion executed")
	}
}
