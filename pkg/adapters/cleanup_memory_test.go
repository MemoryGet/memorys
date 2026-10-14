package adapters

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/index"
)

func memoryCleanupFixture(t *testing.T) (MemoryCleanup, contracts.CleanupOperation, index.Record) {
	t.Helper()
	m := &index.MemoryIndex{AuthorizeDelete: func(context.Context, contracts.AccessScope, contracts.ID) error { return nil }}
	r := index.Record{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc", GenerationID: "generation", BackendEpochID: "epoch", Published: true, Chunk: contracts.Chunk{ID: "chunk", RevisionID: "revision", Text: "text", Spans: []contracts.BlockSpan{{BlockID: "block"}}}, Citation: contracts.Citation{DocumentID: "doc", RevisionID: "revision", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block"}}}
	if err := m.Upsert(r); err != nil {
		t.Fatal(err)
	}
	g, b, c := contracts.ID("generation"), contracts.ID("epoch"), contracts.KeywordChannel
	digest := contracts.Digest(strings.Repeat("a", 64))
	op, err := cleanup.Operation(contracts.CleanupLease{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc", PlanID: "plan", TargetID: "target", RevisionID: "revision", GenerationSetID: "set", PublicationEpoch: 1, Target: contracts.CleanupTarget{ID: "target", Kind: "index", ModuleInstanceID: "engine", BindingReference: "binding", ProfileDigest: digest, GenerationID: &g, BackendEpochID: &b, Channel: &c}})
	if err != nil {
		t.Fatal(err)
	}
	a := MemoryCleanup{Index: m, ModuleInstanceID: "engine", ProfileDigest: digest, Authorize: func(ctx context.Context, op contracts.CleanupOperation) (contracts.IndexDeleteRequest, error) {
		deadline, _ := ctx.Deadline()
		return contracts.IndexDeleteRequest{GenerationID: *op.Target.GenerationID, Request: contracts.RequestContext{RequestID: "request", ProfileDigest: op.Target.ProfileDigest, Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "index-scope", SubjectID: "worker", SpaceID: op.SpaceID, KnowledgeBaseIDs: []contracts.ID{op.KnowledgeBaseID}, Action: "index.delete", PolicyVersion: 1, ExpiresAt: deadline}}, nil
	}}
	return a, op, r
}
func TestMemoryCleanupActualEffectAndNewGenerationIsolation(t *testing.T) {
	a, op, old := memoryCleanupFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	newer := old
	newer.GenerationID = "new-generation"
	newer.BackendEpochID = "new-epoch"
	newer.Chunk.RevisionID = "new-revision"
	newer.Citation.RevisionID = "new-revision"
	if err := a.Index.(*index.MemoryIndex).Upsert(newer); err != nil {
		t.Fatal(err)
	}
	before, err := a.Lookup(ctx, op)
	if err != nil || before.State != "absent" {
		t.Fatal(before, err)
	}
	forged := before
	forged.State = "succeeded"
	forged.ResultReference = resultReference(op)
	if a.VerifyReceipt(ctx, op, forged) == nil {
		t.Fatal("undeleted generation accepted")
	}
	for i := 0; i < 2; i++ {
		r, err := a.Execute(ctx, op)
		if err != nil || r.State != "succeeded" || a.VerifyReceipt(ctx, op, r) != nil {
			t.Fatal("delete/replay failed", err)
		}
	}
	if a.Index.(*index.MemoryIndex).Upsert(old) == nil {
		t.Fatal("late write resurrected old generation")
	}
	scope := contracts.AccessScope{ScopeID: "inspect", SubjectID: "worker", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "index.delete", PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}
	state, err := a.Index.InspectGeneration(ctx, scope, "new-generation")
	if err != nil || state.Deleted || state.RecordCount != 1 {
		t.Fatal("new generation affected", state, err)
	}
}
func TestMemoryCleanupRejectsWrongGrantAndMissingInstance(t *testing.T) {
	a, op, _ := memoryCleanupFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	issue := a.Authorize
	a.Authorize = func(c context.Context, v contracts.CleanupOperation) (contracts.IndexDeleteRequest, error) {
		r, e := issue(c, v)
		r.Scope.Action = "publication.cleanup.execute"
		return r, e
	}
	if _, err := a.Execute(ctx, op); err == nil {
		t.Fatal("relabelled task scope accepted")
	}
	a.Authorize = issue
	a.ModuleInstanceID = "other"
	if _, err := a.Lookup(ctx, op); err == nil {
		t.Fatal("wrong module accepted")
	}
	a.ModuleInstanceID = "engine"
	a.Index.(*index.MemoryIndex).AuthorizeDelete = nil
	if _, err := a.Lookup(ctx, op); err == nil {
		t.Fatal("missing backend authority accepted")
	}
}
