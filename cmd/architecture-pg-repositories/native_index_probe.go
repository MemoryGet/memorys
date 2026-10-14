package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/index"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

func verifyNativeIndexCleanup(ctx context.Context, db *sql.DB, scope contracts.AccessScope) error {
	// Reclaim the earlier race-test lease; this is a fixture transition only.
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.cleanup_progress SET lease_until=clock_timestamp()-interval '1 second' WHERE plan_id='plan-success' AND target_id='target' AND state='running'`); err != nil {
		return err
	}
	scope.SubjectID = "worker-a"
	scope.Action = "publication.cleanup.execute"
	verifyTask := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "worker-a" || s.Action != "publication.cleanup.execute" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("fixture task authority denied")
		}
		return nil
	}
	verifyIndex := func(c context.Context, s contracts.AccessScope, g contracts.ID) error {
		if c.Err() != nil || s.ScopeID != "native-index-grant" || s.SubjectID != "worker-a" || s.Action != "index.delete" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || g != "generation" && g != "new-generation" {
			return errors.New("fixture index authority denied")
		}
		return nil
	}
	native := &index.MemoryIndex{AuthorizeDelete: verifyIndex}
	old := index.Record{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc", GenerationID: "generation", BackendEpochID: "epoch", Published: true, Chunk: contracts.Chunk{ID: "old-chunk", RevisionID: "revision", Text: "old text", Spans: []contracts.BlockSpan{{BlockID: "block"}}}, Citation: contracts.Citation{DocumentID: "doc", RevisionID: "revision", BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block"}}}
	newer := old
	newer.GenerationID = "new-generation"
	newer.BackendEpochID = "new-epoch"
	newer.Chunk.RevisionID = "new-revision"
	newer.Citation.RevisionID = "new-revision"
	newer.Chunk.ID = "new-chunk"
	if native.Upsert(old) != nil || native.Upsert(newer) != nil {
		return errors.New("native index seed failed")
	}
	adapter := adapters.MemoryCleanup{Index: native, ModuleInstanceID: "engine", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Authorize: func(c context.Context, op contracts.CleanupOperation) (contracts.IndexDeleteRequest, error) {
		if op.Target.BindingReference != "binding" || op.DocumentID != "doc" || op.RevisionID != "revision" || op.Target.GenerationID == nil || *op.Target.GenerationID != "generation" {
			return contracts.IndexDeleteRequest{}, errors.New("fixture fixed index binding denied")
		}
		deadline, _ := c.Deadline()
		return contracts.IndexDeleteRequest{GenerationID: "generation", Request: contracts.RequestContext{RequestID: op.ID, ProfileDigest: op.Target.ProfileDigest, Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "native-index-grant", SubjectID: "worker-a", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, Action: "index.delete", ExpiresAt: deadline}}, nil
	}}
	store := postgres.CleanupTasks{DB: db, Verify: verifyTask}
	w := cleanup.Worker{Tasks: store, VerifyLease: store.VerifyLease, Resolve: func(_ context.Context, op contracts.CleanupOperation) (contracts.CleanupExecutor, error) {
		if op.Target.ModuleInstanceID != "engine" {
			return nil, errors.New("wrong index module")
		}
		return adapter, nil
	}, AuthorizeExecution: func(context.Context, contracts.AccessScope, contracts.CleanupLease, contracts.CleanupOperation) error {
		return nil
	}, VerifyReceipt: adapter.VerifyReceipt}
	if err := w.RunOne(ctx, scope, "plan-success", "target", "worker-a", 2*time.Second); err != nil {
		return err
	}
	if native.Upsert(old) == nil {
		return errors.New("late old projection resurrected")
	}
	indexScope := scope
	indexScope.ScopeID = "native-index-grant"
	indexScope.Action = "index.delete"
	oldState, err := native.InspectGeneration(ctx, indexScope, "generation")
	if err != nil || !oldState.Deleted || oldState.RecordCount != 0 {
		return errors.New("old generation not actually deleted")
	}
	newState, err := native.InspectGeneration(ctx, indexScope, "new-generation")
	if err != nil || newState.Deleted || newState.RecordCount != 1 {
		return errors.New("new generation affected by old cleanup")
	}
	var state string
	if db.QueryRowContext(ctx, `SELECT state FROM akos_withdraw_v1.cleanup_progress WHERE plan_id='plan-success' AND target_id='target'`).Scan(&state) != nil || state != "succeeded" {
		return errors.New("native cleanup progress did not complete")
	}
	return nil
}
