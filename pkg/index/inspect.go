package index

import (
	"context"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// GenerationState reports actual owner/tombstone facts, not search absence.
// It is process-local; it provides no persistence or restart recovery proof.
type GenerationState = contracts.IndexDeletionState

func (m *MemoryIndex) InspectGeneration(ctx context.Context, scope contracts.AccessScope, generation contracts.ID) (GenerationState, error) {
	var state GenerationState
	if ctx == nil || m == nil || m.AuthorizeDelete == nil || generation == "" || scope.ScopeID == "" || scope.SubjectID == "" || scope.SpaceID == "" || scope.PolicyVersion == 0 || scope.Action != "index.delete" || !scope.ExpiresAt.After(time.Now()) {
		return state, ErrIndex
	}
	if _, ok := ctx.Deadline(); !ok {
		return state, ErrIndex
	}
	ctx, cancel := context.WithDeadline(ctx, scope.ExpiresAt)
	defer cancel()
	allowed, ok := ids(scope.KnowledgeBaseIDs)
	if !ok {
		return state, ErrIndex
	}
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	if m.AuthorizeDelete(ctx, scope, generation) != nil || ctx.Err() != nil {
		return state, ErrIndex
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	owner, known := m.owners[generation]
	if !known || owner.SpaceID != scope.SpaceID || !allowed[owner.KnowledgeBaseID] || ctx.Err() != nil {
		return state, ErrIndex
	}
	state = GenerationState{SpaceID: owner.SpaceID, KnowledgeBaseID: owner.KnowledgeBaseID, DocumentID: owner.DocumentID, RevisionID: owner.RevisionID, GenerationID: generation, BackendEpochID: owner.BackendEpochID, Deleted: m.deleted[generation]}
	for key := range m.records {
		if key[0] == generation {
			state.RecordCount++
		}
	}
	return state, nil
}
