package index_test

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/index"
	"strings"
	"sync"
	"testing"
	"time"
)

func request() contracts.RetrieveRequest {
	return contracts.RetrieveRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "user", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "knowledge.search", PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}, Publication: contracts.PublicationFilter{SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, RevisionIDs: []contracts.ID{"revision"}, GenerationIDs: []contracts.ID{"generation"}, SnapshotDigest: contracts.Digest(strings.Repeat("a", 64))}, Generations: []contracts.IndexGeneration{{ID: "generation", KnowledgeBaseID: "kb", RevisionID: "revision", BackendEpochID: "epoch", Channel: contracts.KeywordChannel, State: "ready"}}, BackendEpochID: "epoch", RouteID: "route", Channels: []contracts.Channel{contracts.KeywordChannel}, Query: contracts.Query{Text: "text"}, Limit: 1}
}

func TestMemoryIndexConcurrentProjectionChangesAndSearch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m := &index.MemoryIndex{}
	var wg sync.WaitGroup
	for worker := 0; worker < 6; worker++ {
		wg.Add(1)
		go func(writer bool) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if writer {
					r := record("concurrent")
					r.Published = i%2 == 0
					if err := m.Upsert(r); err != nil {
						t.Error(err)
					}
				} else {
					results, err := m.Search(ctx, request())
					if err != nil || len(results) > 1 {
						t.Error("concurrent search", err)
					}
				}
			}
		}(worker%2 == 0)
	}
	wg.Wait()
}
func record(id string) index.Record {
	return index.Record{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: contracts.ID(id), GenerationID: "generation", BackendEpochID: "epoch", Published: true, Chunk: contracts.Chunk{ID: contracts.ID(id), RevisionID: "revision", Text: "private text", Spans: []contracts.BlockSpan{{BlockID: "block"}}}, Citation: contracts.Citation{DocumentID: contracts.ID(id), RevisionID: "revision", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block", Locator: contracts.Locator{Precision: "exact", Characters: &contracts.CharacterRange{Start: 0, End: 12}}}}}
}
func TestMemoryIndexFiltersBeforeLimitAndPreservesOwnedCitation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m := &index.MemoryIndex{}
	for _, kind := range []string{"space", "kb", "draft", "revision", "generation", "epoch"} {
		r := record(kind)
		r.GenerationID = contracts.ID(kind)
		switch kind {
		case "space":
			r.SpaceID = "other"
		case "kb":
			r.KnowledgeBaseID = "other"
		case "draft":
			r.Published = false
		case "revision":
			r.Chunk.RevisionID = "draft"
			r.Citation.RevisionID = "draft"
		case "generation":
			r.GenerationID = "other"
		case "epoch":
			r.BackendEpochID = "other"
		}
		if err := m.Upsert(r); err != nil {
			t.Fatal(err)
		}
	}
	r := record("allowed")
	if err := m.Upsert(r); err != nil {
		t.Fatal(err)
	}
	r.Citation.Source.Locator.Characters.End = 0
	results, err := m.Search(ctx, request())
	if err != nil || len(results) != 1 || results[0].Citation.DocumentID != "allowed" || results[0].Citation.Source.Locator.Characters.End != 12 {
		t.Fatal("scope/publishing/citation leak", err, results)
	}
	results[0].Citation.Source.Locator.Characters.End = 0
	again, _ := m.Search(ctx, request())
	if again[0].Citation.Source.Locator.Characters.End != 12 {
		t.Fatal("result aliases index")
	}
}
func TestMemoryIndexRejectsUnfilteredEmptyExpiredAndCancelledRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m := &index.MemoryIndex{}
	for _, change := range []func(*contracts.RetrieveRequest){func(r *contracts.RetrieveRequest) { r.Scope = contracts.AccessScope{} }, func(r *contracts.RetrieveRequest) { r.Publication = contracts.PublicationFilter{} }, func(r *contracts.RetrieveRequest) { r.Query.Text = "" }, func(r *contracts.RetrieveRequest) { r.Scope.ExpiresAt = time.Now().Add(-time.Second) }, func(r *contracts.RetrieveRequest) { r.Publication.KnowledgeBaseIDs = []contracts.ID{"foreign"} }, func(r *contracts.RetrieveRequest) { r.Generations = nil }} {
		req := request()
		change(&req)
		if _, err := m.Search(ctx, req); err == nil {
			t.Fatal("unsafe request accepted")
		}
	}
	cancel()
	if _, err := m.Search(ctx, request()); err != context.Canceled {
		t.Fatal("cancel ignored")
	}
}
