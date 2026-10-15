package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/index"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

func verifyIndexTransfer(ctx context.Context, db *sql.DB, scope contracts.AccessScope) error {
	scope.SubjectID = "migration-worker"
	scope.Action = "index.export"
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SubjectID != "migration-worker" || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" || s.Action != "index.export" && s.Action != "index.import" {
			return errors.New("fixture transfer authority denied")
		}
		return nil
	}
	deleteScope := scope
	deleteScope.Action = "index.delete"
	m := &index.MemoryIndex{AuthorizeSnapshot: verify, AuthorizeDelete: func(context.Context, contracts.AccessScope, contracts.ID) error { return nil }}
	record := func(kind, chunk string) index.Record {
		return index.Record{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "transfer-doc", GenerationID: contracts.ID("transfer-" + kind), BackendEpochID: contracts.ID("transfer-" + kind + "-epoch"), Published: true, Chunk: contracts.Chunk{ID: contracts.ID(chunk), RevisionID: contracts.ID("transfer-" + kind + "-rev"), Text: "中文知识资料", Spans: []contracts.BlockSpan{{BlockID: "block"}}}, Citation: contracts.Citation{DocumentID: "transfer-doc", RevisionID: contracts.ID("transfer-" + kind + "-rev"), BlockID: "block", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "block"}}}
	}
	old, live, gone, kept := record("old", "old"), record("live", "live"), record("chunks", "gone"), record("chunks", "kept")
	for _, r := range []index.Record{old, live, gone, kept} {
		if m.Upsert(r) != nil {
			return errors.New("transfer native setup failed")
		}
	}
	deadline, _ := ctx.Deadline()
	request := contracts.IndexDeleteRequest{Scope: deleteScope, Request: contracts.RequestContext{RequestID: "transfer-delete", Deadline: deadline}, GenerationID: old.GenerationID}
	if m.Delete(ctx, request) != nil {
		return errors.New("transfer deletion failed")
	}
	request.GenerationID = gone.GenerationID
	request.ChunkIDs = []contracts.ID{gone.Chunk.ID}
	if m.Delete(ctx, request) != nil {
		return errors.New("transfer chunk deletion failed")
	}
	ids := []contracts.ID{old.GenerationID, live.GenerationID, gone.GenerationID}
	snapshot, err := m.ExportIndex(ctx, scope, ids)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.documents VALUES('space','kb','transfer-doc',0,1,NULL,NULL)`); err != nil {
		return err
	}
	for _, kind := range []string{"old", "live", "chunks"} {
		gen, rev, set, epoch := "transfer-"+kind, "transfer-"+kind+"-rev", "transfer-"+kind+"-set", "transfer-"+kind+"-epoch"
		if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.revisions VALUES('space','kb','transfer-doc',$1::text,$2::text)`, rev, strings.Repeat("a", 64)); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.generation_sets VALUES('space','kb','transfer-doc',$1::text,$2::text,$3::text)`, rev, set, epoch); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.index_generations(space_id,kb_id,document_id,revision_id,generation_set_id,id,backend_epoch_id,channel,profile_digest,module_instance_id) VALUES('space','kb','transfer-doc',$1::text,$2::text,$3::text,$4::text,'keyword',$5::text,'engine')`, rev, set, gen, epoch, strings.Repeat("a", 64)); err != nil {
			return err
		}
	}
	pg := postgres.IndexCleanup{DB: db, Verify: verify}
	importScope := scope
	importScope.Action = "index.import"
	// Fail after projection INSERTs, during deletion-fence restore; all data rolls back.
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.index_generations SET projection_epoch=18446744073709551615 WHERE id='transfer-old'`); err != nil {
		return err
	}
	if pg.ImportIndex(ctx, importScope, snapshot) == nil {
		return errors.New("failing import committed")
	}
	var count int
	if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.index_projection_rows WHERE generation_id IN('transfer-old','transfer-live','transfer-chunks')`).Scan(&count) != nil || count != 0 {
		return errors.New("partial import projections survived")
	}
	if _, err := db.ExecContext(ctx, `UPDATE akos_withdraw_v1.index_generations SET projection_epoch=0 WHERE id='transfer-old'`); err != nil {
		return err
	}
	if err := pg.ImportIndex(ctx, importScope, snapshot); err != nil {
		return err
	}
	back, err := pg.ExportIndex(ctx, scope, ids)
	if err != nil || back.Checksum != snapshot.Checksum {
		return errors.New("native to postgres snapshot changed")
	}
	cold := &index.MemoryIndex{AuthorizeSnapshot: verify}
	if err := cold.ImportIndex(ctx, importScope, back); err != nil {
		return err
	}
	returned, err := cold.ExportIndex(ctx, scope, ids)
	if err != nil || returned.Checksum != snapshot.Checksum {
		return errors.New("postgres to native snapshot changed")
	}
	if cold.Upsert(old) == nil || cold.Upsert(gone) == nil {
		return errors.New("restored tombstone lost")
	}
	if cold.Upsert(live) != nil || cold.Upsert(kept) != nil {
		return errors.New("restored live records rejected")
	}
	corrupt := snapshot
	corrupt.Checksum = "bad"
	empty := &index.MemoryIndex{AuthorizeSnapshot: verify}
	if empty.ImportIndex(ctx, importScope, corrupt) == nil {
		return errors.New("corrupt snapshot accepted")
	}
	if cold.ImportIndex(ctx, importScope, snapshot) == nil || pg.ImportIndex(ctx, importScope, snapshot) == nil {
		return errors.New("occupied target overwritten")
	}
	// Test actual restored SQL masks, not just exported flags.
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.index_projection_rows VALUES('transfer-old','late','{}')`); err == nil {
		return errors.New("restored generation fence absent")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.index_projection_rows VALUES('transfer-chunks','gone','{}')`); err == nil {
		return errors.New("restored chunk fence absent")
	}
	if err := verifyConfiguredIndexStores(ctx, db, scope, ids, m, pg, snapshot.Checksum); err != nil {
		return err
	}
	return nil
}
