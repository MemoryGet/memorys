package postgres

import (
	"context"
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/indexsnapshot"
)

func (a IndexCleanup) ExportIndex(ctx context.Context, scope contracts.AccessScope, ids []contracts.ID) (contracts.IndexSnapshot, error) {
	scope = cloneScope(scope)
	ids = append([]contracts.ID(nil), ids...)
	var snapshot contracts.IndexSnapshot
	if scope.Action != "index.export" || len(ids) < 1 || len(ids) > 1000 {
		return snapshot, ErrRepository
	}
	err := withinSession(ctx, a.DB, scope, a.Verify, func(c context.Context, s *session) error {
		if err := s.enter(c, scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		snapshot = contracts.IndexSnapshot{Format: indexsnapshot.Format, SpaceID: scope.SpaceID, KnowledgeBaseID: scope.KnowledgeBaseIDs[0]}
		seen := map[contracts.ID]bool{}
		for _, id := range ids {
			if seen[id] {
				return ErrRepository
			}
			seen[id] = true
			var g contracts.IndexDeletionState
			if s.tx.QueryRowContext(c, `SELECT space_id,kb_id,document_id,revision_id,id,backend_epoch_id,EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id),(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=g.id) FROM akos_withdraw_v1.index_generations g WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(id)).Scan(&g.SpaceID, &g.KnowledgeBaseID, &g.DocumentID, &g.RevisionID, &g.GenerationID, &g.BackendEpochID, &g.Deleted, &g.RecordCount) != nil {
				return ErrRepository
			}
			snapshot.Generations = append(snapshot.Generations, g)
			rows, err := s.tx.QueryContext(c, `SELECT payload FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=$1::text ORDER BY chunk_id LIMIT 10001`, string(id))
			if err != nil {
				return ErrRepository
			}
			for rows.Next() {
				var raw []byte
				var record contracts.IndexProjection
				if rows.Scan(&raw) != nil || len(raw) > 4<<20 || json.Unmarshal(raw, &record) != nil {
					rows.Close()
					return ErrRepository
				}
				snapshot.Records = append(snapshot.Records, record)
				if len(snapshot.Records) > 10000 {
					rows.Close()
					return ErrRepository
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return ErrRepository
			}
			rows, err = s.tx.QueryContext(c, `SELECT chunk_id FROM akos_withdraw_v1.index_chunk_deletion_fences WHERE generation_id=$1::text ORDER BY chunk_id LIMIT 10001`, string(id))
			if err != nil {
				return ErrRepository
			}
			for rows.Next() {
				f := contracts.IndexChunkFence{GenerationID: id}
				if rows.Scan(&f.ChunkID) != nil {
					rows.Close()
					return ErrRepository
				}
				snapshot.ChunkFences = append(snapshot.ChunkFences, f)
				if len(snapshot.ChunkFences) > 10000 {
					rows.Close()
					return ErrRepository
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return ErrRepository
			}
		}
		var err error
		snapshot, err = indexsnapshot.Seal(snapshot)
		if err != nil {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return contracts.IndexSnapshot{}, err
	}
	return snapshot, nil
}
func (a IndexCleanup) ImportIndex(ctx context.Context, scope contracts.AccessScope, input contracts.IndexSnapshot) error {
	scope = cloneScope(scope)
	snapshot, err := indexsnapshot.Verify(input)
	if err != nil || scope.Action != "index.import" || len(scope.KnowledgeBaseIDs) != 1 || snapshot.SpaceID != scope.SpaceID || snapshot.KnowledgeBaseID != scope.KnowledgeBaseIDs[0] {
		return ErrRepository
	}
	return withinSession(ctx, a.DB, scope, a.Verify, func(c context.Context, s *session) error {
		if err := s.enter(c, scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		for _, g := range snapshot.Generations {
			var doc, revision, epoch contracts.ID
			if s.tx.QueryRowContext(c, `SELECT document_id,revision_id,backend_epoch_id FROM akos_withdraw_v1.index_generations WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR UPDATE`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(g.GenerationID)).Scan(&doc, &revision, &epoch) != nil || doc != g.DocumentID || revision != g.RevisionID || epoch != g.BackendEpochID {
				return ErrRepository
			}
			var empty bool
			if s.tx.QueryRowContext(c, `SELECT NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=$1::text) AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences WHERE generation_id=$1::text) AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_chunk_deletion_fences WHERE generation_id=$1::text)`, string(g.GenerationID)).Scan(&empty) != nil || !empty {
				return ErrRepository
			}
		}
		for _, r := range snapshot.Records {
			raw, err := json.Marshal(r)
			if err != nil {
				return ErrRepository
			}
			if _, err := s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.index_projection_rows(generation_id,chunk_id,payload) VALUES($1::text,$2::text,$3::jsonb)`, string(r.GenerationID), string(r.Chunk.ID), string(raw)); err != nil {
				return ErrRepository
			}
		}
		for _, g := range snapshot.Generations {
			if g.Deleted {
				if _, err := s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.index_deletion_fences(generation_id) VALUES($1::text)`, string(g.GenerationID)); err != nil {
					return ErrRepository
				}
			}
		}
		for _, f := range snapshot.ChunkFences {
			if _, err := s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.index_chunk_deletion_fences(generation_id,chunk_id) VALUES($1::text,$2::text)`, string(f.GenerationID), string(f.ChunkID)); err != nil {
				return ErrRepository
			}
		}
		return nil
	})
}
