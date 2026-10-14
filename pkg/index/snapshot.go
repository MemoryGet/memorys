package index

import (
	"context"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/indexsnapshot"
)

func (m *MemoryIndex) snapshotScope(ctx context.Context, s contracts.AccessScope, action contracts.Action) error {
	if ctx == nil || m == nil || m.AuthorizeSnapshot == nil || s.Action != action || s.SubjectID == "" || s.ScopeID == "" || s.PolicyVersion == 0 || len(s.KnowledgeBaseIDs) != 1 || !s.ExpiresAt.After(time.Now()) {
		return ErrIndex
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrIndex
	}
	s.KnowledgeBaseIDs = append([]contracts.ID(nil), s.KnowledgeBaseIDs...)
	if m.AuthorizeSnapshot(ctx, s) != nil || ctx.Err() != nil {
		return ErrIndex
	}
	return nil
}
func (m *MemoryIndex) ExportIndex(ctx context.Context, scope contracts.AccessScope, generations []contracts.ID) (contracts.IndexSnapshot, error) {
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	if err := m.snapshotScope(ctx, scope, "index.export"); err != nil {
		return contracts.IndexSnapshot{}, err
	}
	selected, ok := ids(generations)
	if !ok {
		return contracts.IndexSnapshot{}, ErrIndex
	}
	snapshot, err := m.exportIndexData(ctx, scope, selected)
	if err != nil {
		return contracts.IndexSnapshot{}, err
	}
	if err := m.snapshotScope(ctx, scope, "index.export"); err != nil {
		return contracts.IndexSnapshot{}, err
	}
	return snapshot, nil
}
func (m *MemoryIndex) exportIndexData(ctx context.Context, scope contracts.AccessScope, selected map[contracts.ID]bool) (contracts.IndexSnapshot, error) {
	s := contracts.IndexSnapshot{Format: indexsnapshot.Format, SpaceID: scope.SpaceID, KnowledgeBaseID: scope.KnowledgeBaseIDs[0]}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for gen := range selected {
		owner, ok := m.owners[gen]
		if !ok || owner.SpaceID != s.SpaceID || owner.KnowledgeBaseID != s.KnowledgeBaseID {
			return contracts.IndexSnapshot{}, ErrIndex
		}
		g := contracts.IndexDeletionState{SpaceID: owner.SpaceID, KnowledgeBaseID: owner.KnowledgeBaseID, DocumentID: owner.DocumentID, RevisionID: owner.RevisionID, GenerationID: gen, BackendEpochID: owner.BackendEpochID, Deleted: m.deleted[gen]}
		for key, r := range m.records {
			if key[0] == gen {
				s.Records = append(s.Records, r)
				g.RecordCount++
			}
		}
		s.Generations = append(s.Generations, g)
	}
	for key := range m.deletedChunks {
		if selected[key[0]] {
			s.ChunkFences = append(s.ChunkFences, contracts.IndexChunkFence{GenerationID: key[0], ChunkID: key[1]})
		}
	}
	if ctx.Err() != nil || !scope.ExpiresAt.After(time.Now()) {
		return contracts.IndexSnapshot{}, ErrIndex
	}
	return indexsnapshot.Seal(s)
}
func (m *MemoryIndex) ImportIndex(ctx context.Context, scope contracts.AccessScope, input contracts.IndexSnapshot) error {
	scope.KnowledgeBaseIDs = append([]contracts.ID(nil), scope.KnowledgeBaseIDs...)
	if err := m.snapshotScope(ctx, scope, "index.import"); err != nil {
		return err
	}
	s, err := indexsnapshot.Verify(input)
	if err != nil || s.SpaceID != scope.SpaceID || s.KnowledgeBaseID != scope.KnowledgeBaseIDs[0] {
		return ErrIndex
	}
	staged := &MemoryIndex{}
	for _, r := range s.Records {
		if staged.Upsert(r) != nil {
			return ErrIndex
		}
	}
	if staged.owners == nil {
		staged.owners = map[contracts.ID]generationOwner{}
	}
	staged.deleted = map[contracts.ID]bool{}
	staged.deletedChunks = map[[2]contracts.ID]bool{}
	for _, g := range s.Generations {
		staged.owners[g.GenerationID] = generationOwner{g.SpaceID, g.KnowledgeBaseID, g.DocumentID, g.RevisionID, g.BackendEpochID}
		if g.Deleted {
			staged.deleted[g.GenerationID] = true
		}
	}
	for _, f := range s.ChunkFences {
		staged.deletedChunks[[2]contracts.ID{f.GenerationID, f.ChunkID}] = true
	}
	if err := m.snapshotScope(ctx, scope, "index.import"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.owners) != 0 || ctx.Err() != nil || !scope.ExpiresAt.After(time.Now()) {
		return ErrIndex
	}
	m.records, m.order, m.owners, m.deleted, m.deletedChunks = staged.records, staged.order, staged.owners, staged.deleted, staged.deletedChunks
	return nil
}
