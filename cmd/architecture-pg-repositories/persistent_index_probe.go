package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/MemoryGet/memorys/pkg/adapters"
	"github.com/MemoryGet/memorys/pkg/cleanup"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

func verifyPersistentIndex(ctx context.Context, db *sql.DB, scope contracts.AccessScope) error {
	seed := []string{
		`INSERT INTO akos_withdraw_v1.revisions VALUES('space','kb','go-plan-doc','pg-new-revision',repeat('a',64))`,
		`INSERT INTO akos_withdraw_v1.generation_sets VALUES('space','kb','go-plan-doc','pg-new-revision','pg-new-set','pg-new-epoch')`,
		`INSERT INTO akos_withdraw_v1.index_generations(space_id,kb_id,document_id,revision_id,generation_set_id,id,backend_epoch_id,channel,profile_digest,module_instance_id) VALUES('space','kb','go-plan-doc','pg-new-revision','pg-new-set','pg-new-generation','pg-new-epoch','keyword',repeat('a',64),'engine')`,
		`INSERT INTO akos_withdraw_v1.index_projection_rows VALUES('go-plan-gen','old-chunk','{"text":"old"}')`,
		`INSERT INTO akos_withdraw_v1.index_projection_rows VALUES('pg-new-generation','new-chunk','{"text":"new"}')`,
	}
	for _, statement := range seed {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return errors.New("persistent index seed failed")
		}
	}
	scope.SubjectID = "worker-a"
	scope.ScopeID = "persistent-index-grant"
	scope.Action = "index.delete"
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.ScopeID != "persistent-index-grant" || s.SubjectID != "worker-a" || s.Action != "index.delete" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("fixture persistent index authority denied")
		}
		return nil
	}
	backend := postgres.IndexCleanup{DB: db, Verify: verify}
	g, e, ch := contracts.ID("go-plan-gen"), contracts.ID("go-plan-epoch"), contracts.KeywordChannel
	op, err := cleanup.Operation(contracts.CleanupLease{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "go-plan-doc", PlanID: "go-plan", TargetID: "go-plan-binding", RevisionID: "go-plan-rev", GenerationSetID: "go-plan-set", PublicationEpoch: 1, Target: contracts.CleanupTarget{ID: "go-plan-binding", Kind: "index", ModuleInstanceID: "engine", BindingReference: "go-plan-binding", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), GenerationID: &g, BackendEpochID: &e, Channel: &ch}})
	if err != nil {
		return err
	}
	adapter := adapters.IndexCleanup{Index: backend, ModuleInstanceID: "engine", ProfileDigest: op.Target.ProfileDigest, Authorize: func(c context.Context, v contracts.CleanupOperation) (contracts.IndexDeleteRequest, error) {
		deadline, _ := c.Deadline()
		return contracts.IndexDeleteRequest{GenerationID: *v.Target.GenerationID, Scope: scope, Request: contracts.RequestContext{RequestID: v.ID, ProfileDigest: v.Target.ProfileDigest, Deadline: deadline}}, nil
	}}
	// A writer holding an old Repeatable Read snapshot must not bypass deletion.
	writer, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer writer.Rollback()
	var epoch int
	if writer.QueryRowContext(ctx, `SELECT projection_epoch FROM akos_withdraw_v1.index_generations WHERE id='go-plan-gen'`).Scan(&epoch) != nil {
		return errors.New("writer snapshot failed")
	}
	receipt, err := adapter.Execute(ctx, op)
	if err != nil || adapter.VerifyReceipt(ctx, op, receipt) != nil {
		return errors.New("persistent deletion failed")
	}
	_, err = writer.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.index_projection_rows VALUES('go-plan-gen','stale-chunk','{}')`)
	var stateError interface{ SQLState() string }
	if !errors.As(err, &stateError) || stateError.SQLState() != "40001" {
		return errors.New("old snapshot bypassed generation fence")
	}
	writer.Rollback()
	other, err := sql.Open("pgx", "postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable")
	if err != nil {
		return err
	}
	defer other.Close()
	cold := postgres.IndexCleanup{DB: other, Verify: verify}
	adapter.Index = cold
	replayed, err := adapter.Lookup(ctx, op)
	if err != nil || replayed != receipt || adapter.VerifyReceipt(ctx, op, replayed) != nil {
		return errors.New("persisted deletion lost on reconnect")
	}
	newState, err := cold.InspectGeneration(ctx, scope, "pg-new-generation")
	if err != nil || newState.Deleted || newState.RecordCount != 1 {
		return errors.New("new persistent generation affected")
	}
	if _, err := other.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.index_projection_rows VALUES('go-plan-gen','late-chunk','{}')`); err == nil {
		return errors.New("late persistent write resurrected generation")
	}
	return nil
}
