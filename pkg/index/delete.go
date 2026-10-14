package index

import (
	"context"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type generationOwner struct{ SpaceID, KnowledgeBaseID, DocumentID, RevisionID, BackendEpochID contracts.ID }

// Delete uses the native contract: empty ChunkIDs means the single named
// generation; a nonempty list means only those chunks. Tombstones block stale
// Upsert calls during this process lifetime. Persistence is not provided here.
func (m *MemoryIndex) Delete(ctx context.Context, req contracts.IndexDeleteRequest) error {
	if ctx == nil || m.AuthorizeDelete == nil || req.Request.RequestID == "" || req.Request.Deadline.IsZero() || req.GenerationID == "" || req.Scope.ScopeID == "" || req.Scope.SubjectID == "" || req.Scope.SpaceID == "" || req.Scope.Action != "index.delete" || req.Scope.PolicyVersion == 0 || !req.Scope.ExpiresAt.After(time.Now()) {
		return ErrIndex
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrIndex
	}
	deadline := req.Request.Deadline
	if req.Scope.ExpiresAt.Before(deadline) {
		deadline = req.Scope.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	allowed, ok := ids(req.Scope.KnowledgeBaseIDs)
	if !ok {
		return ErrIndex
	}
	selected := map[contracts.ID]bool{}
	if len(req.ChunkIDs) > 0 {
		selected, ok = ids(req.ChunkIDs)
		if !ok {
			return ErrIndex
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	scope := req.Scope
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	if m.AuthorizeDelete(ctx, scope, req.GenerationID) != nil {
		return ErrIndex
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	owner, known := m.owners[req.GenerationID]
	// Unknown generations cannot be silently accepted: no ownership proof.
	if !known || owner.SpaceID != req.Scope.SpaceID || !allowed[owner.KnowledgeBaseID] {
		return ErrIndex
	}
	if m.deleted == nil {
		m.deleted = make(map[contracts.ID]bool)
	}
	if m.deletedChunks == nil {
		m.deletedChunks = make(map[[2]contracts.ID]bool)
	}
	if len(selected) == 0 {
		m.deleted[req.GenerationID] = true
	} else {
		for id := range selected {
			m.deletedChunks[[2]contracts.ID{req.GenerationID, id}] = true
		}
	}
	kept := m.order[:0]
	for _, key := range m.order {
		if key[0] == req.GenerationID && (len(selected) == 0 || selected[key[1]]) {
			delete(m.records, key)
		} else {
			kept = append(kept, key)
		}
	}
	m.order = kept
	return nil
}
