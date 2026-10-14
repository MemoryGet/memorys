package index

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"strings"
	"sync"
	"time"
)

var ErrIndex = errors.New("index request rejected")

// Record metadata is supplied by the trusted projection publisher, never HTTP.
// Published is only a projection flag; application output still rechecks current publication.
type Record = contracts.IndexProjection
type MemoryIndex struct {
	mu            sync.RWMutex
	records       map[[2]contracts.ID]Record
	order         [][2]contracts.ID
	owners        map[contracts.ID]generationOwner
	deleted       map[contracts.ID]bool
	deletedChunks map[[2]contracts.ID]bool
	// Must verify current control-source authority, not trust Scope fields.
	AuthorizeDelete   func(context.Context, contracts.AccessScope, contracts.ID) error
	AuthorizeSnapshot func(context.Context, contracts.AccessScope) error
}

func (m *MemoryIndex) Upsert(record Record) error {
	if record.SpaceID == "" || record.KnowledgeBaseID == "" || record.DocumentID == "" || record.GenerationID == "" || record.BackendEpochID == "" || record.Chunk.ID == "" || record.Chunk.RevisionID == "" || record.Citation.DocumentID != record.DocumentID || record.Citation.RevisionID != record.Chunk.RevisionID || record.Citation.BlockID == "" || len(record.Chunk.Spans) == 0 || record.Chunk.Spans[0].BlockID != record.Citation.BlockID || record.Citation.Source.Kind != "original" && record.Citation.Source.Kind != "revision" {
		return ErrIndex
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return ErrIndex
	}
	var owned Record
	if json.Unmarshal(raw, &owned) != nil {
		return ErrIndex
	}
	key := [2]contracts.ID{record.GenerationID, record.Chunk.ID}
	m.mu.Lock()
	defer m.mu.Unlock()
	owner := generationOwner{record.SpaceID, record.KnowledgeBaseID, record.DocumentID, record.Chunk.RevisionID, record.BackendEpochID}
	if m.deleted[record.GenerationID] || m.deletedChunks[key] {
		return ErrIndex
	}
	if prior, exists := m.owners[record.GenerationID]; exists && prior != owner {
		return ErrIndex
	}
	if m.owners == nil {
		m.owners = make(map[contracts.ID]generationOwner)
	}
	m.owners[record.GenerationID] = owner
	if m.records == nil {
		m.records = make(map[[2]contracts.ID]Record)
	}
	if prior, ok := m.records[key]; ok {
		if prior.DocumentID != record.DocumentID || prior.SpaceID != record.SpaceID || prior.KnowledgeBaseID != record.KnowledgeBaseID || prior.Chunk.RevisionID != record.Chunk.RevisionID {
			return ErrIndex
		}
	} else {
		m.order = append(m.order, key)
	}
	m.records[key] = owned
	return nil
}
func ids(values []contracts.ID) (map[contracts.ID]bool, bool) {
	if len(values) == 0 || len(values) > 1000 {
		return nil, false
	}
	m := map[contracts.ID]bool{}
	for _, v := range values {
		if v == "" || m[v] {
			return nil, false
		}
		m[v] = true
	}
	return m, true
}
func (m *MemoryIndex) Search(ctx context.Context, req contracts.RetrieveRequest) ([]contracts.Candidate, error) {
	if ctx == nil || req.Query.Text == "" || req.Limit < 1 || req.Limit > 1000 || req.Scope.SubjectID == "" || req.Scope.ScopeID == "" || req.Scope.Action != "knowledge.search" || req.Scope.PolicyVersion == 0 || !req.Scope.ExpiresAt.After(time.Now()) || req.Publication.SpaceID != req.Scope.SpaceID || req.Scope.SpaceID == "" || req.BackendEpochID == "" || req.RouteID == "" || len(req.Channels) != 1 || req.Channels[0] != contracts.KeywordChannel || len(req.Publication.SnapshotDigest) != 64 {
		return nil, ErrIndex
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrIndex
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allowed, ok := ids(req.Scope.KnowledgeBaseIDs)
	if !ok {
		return nil, ErrIndex
	}
	selected, ok := ids(req.Publication.KnowledgeBaseIDs)
	if !ok {
		return nil, ErrIndex
	}
	for kb := range selected {
		if !allowed[kb] {
			return nil, ErrIndex
		}
	}
	revisions, ok := ids(req.Publication.RevisionIDs)
	if !ok {
		return nil, ErrIndex
	}
	generations, ok := ids(req.Publication.GenerationIDs)
	if !ok {
		return nil, ErrIndex
	}
	declared := map[contracts.ID]contracts.IndexGeneration{}
	for _, g := range req.Generations {
		if !generations[g.ID] || !selected[g.KnowledgeBaseID] || !revisions[g.RevisionID] || g.BackendEpochID != req.BackendEpochID || g.Channel != contracts.KeywordChannel || g.State != "ready" {
			return nil, ErrIndex
		}
		if _, duplicate := declared[g.ID]; duplicate {
			return nil, ErrIndex
		}
		declared[g.ID] = g
	}
	if len(declared) != len(generations) {
		return nil, ErrIndex
	}
	query := strings.ToLower(req.Query.Text)
	results := make([]contracts.Candidate, 0)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, key := range m.order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r := m.records[key]
		g, active := declared[r.GenerationID]
		if !active || g.KnowledgeBaseID != r.KnowledgeBaseID || g.RevisionID != r.Chunk.RevisionID {
			continue
		}
		if !r.Published || r.SpaceID != req.Scope.SpaceID || !selected[r.KnowledgeBaseID] || !revisions[r.Chunk.RevisionID] || !generations[r.GenerationID] || r.BackendEpochID != req.BackendEpochID || !strings.Contains(strings.ToLower(r.Chunk.Text), query) {
			continue
		}
		var citation contracts.Citation
		raw, _ := json.Marshal(r.Citation)
		json.Unmarshal(raw, &citation)
		results = append(results, contracts.Candidate{ChunkID: r.Chunk.ID, KnowledgeBaseID: r.KnowledgeBaseID, RouteID: req.RouteID, RetrieverID: "local-memory", Rank: len(results) + 1, Text: r.Chunk.Text, Score: 1, ScoreKind: "keyword", GenerationIDs: []contracts.ID{r.GenerationID}, Citation: citation})
		if len(results) == req.Limit {
			break
		}
	}
	return results, nil
}
