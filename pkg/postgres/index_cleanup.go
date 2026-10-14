package postgres

import (
	"context"
	"database/sql"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type IndexCleanup struct {
	DB     *sql.DB
	Verify VerifyScope
}

func (a IndexCleanup) InspectGeneration(ctx context.Context, scope contracts.AccessScope, generation contracts.ID) (contracts.IndexDeletionState, error) {
	scope = cloneScope(scope)
	var state contracts.IndexDeletionState
	if scope.Action != "index.delete" {
		return state, ErrRepository
	}
	err := withinSession(ctx, a.DB, scope, a.Verify, func(c context.Context, s *session) error {
		if err := s.enter(c, scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		err := s.tx.QueryRowContext(c, `SELECT g.space_id,g.kb_id,g.document_id,g.revision_id,g.id,g.backend_epoch_id,EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id),(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=g.id) FROM akos_withdraw_v1.index_generations g WHERE g.space_id=$1::text AND g.kb_id=$2::text AND g.id=$3::text`, string(scope.SpaceID), string(scope.KnowledgeBaseIDs[0]), string(generation)).Scan(&state.SpaceID, &state.KnowledgeBaseID, &state.DocumentID, &state.RevisionID, &state.GenerationID, &state.BackendEpochID, &state.Deleted, &state.RecordCount)
		if err != nil {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return contracts.IndexDeletionState{}, err
	}
	return state, nil
}
func (a IndexCleanup) Delete(ctx context.Context, req contracts.IndexDeleteRequest) error {
	if req.Scope.Action != "index.delete" || req.Request.RequestID == "" || req.Request.Deadline.IsZero() || req.GenerationID == "" || len(req.ChunkIDs) != 0 || ctx == nil {
		return ErrRepository
	}
	req.Scope = cloneScope(req.Scope)
	ctx, cancel := context.WithDeadline(ctx, req.Request.Deadline)
	defer cancel()
	return withinSession(ctx, a.DB, req.Scope, a.Verify, func(c context.Context, s *session) error {
		if err := s.enter(c, req.Scope); err != nil {
			return err
		}
		defer s.mu.Unlock()
		var found contracts.ID
		if s.tx.QueryRowContext(c, `SELECT id FROM akos_withdraw_v1.index_generations WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR UPDATE`, string(req.Scope.SpaceID), string(req.Scope.KnowledgeBaseIDs[0]), string(req.GenerationID)).Scan(&found) != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(c, `INSERT INTO akos_withdraw_v1.index_deletion_fences(generation_id) VALUES($1::text) ON CONFLICT(generation_id) DO NOTHING`, string(found)); err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(c, `DELETE FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=$1::text`, string(found)); err != nil {
			return ErrRepository
		}
		return nil
	})
}
