package postgres

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/indexsnapshot"
)

// Read all actual native generations/rows needed by this one read view. Native
// fencing, profile/module binding, record limits and sealed snapshot digests
// are identical to loadPreparationSnapshot; no cached data outlives the view.
func (r *queryRepository) prefetchSnapshots(ctx context.Context, facts []contracts.PublishedFact) error {
	if len(facts) < 1 || len(facts) > 1000 {
		return ErrRepository
	}
	byGeneration := map[contracts.ID]contracts.PublishedFact{}
	ids := make([]contracts.ID, 0, len(facts))
	for _, fact := range facts {
		g := fact.GenerationSet.Channels[contracts.KeywordChannel]
		prepared, ok := r.prepared[fact.DocumentID]
		if !ok || len(fact.GenerationSet.Channels) != 1 || g.ID == "" || g.BackendBinding == nil ||
			!reflect.DeepEqual(prepared.artifact.GenerationSet, fact.GenerationSet) || byGeneration[g.ID].DocumentID != "" {
			return ErrRepository
		}
		byGeneration[g.ID] = fact
		ids = append(ids, g.ID)
	}
	rows, err := r.tx.QueryContext(ctx, `SELECT g.space_id,g.kb_id,g.document_id,g.revision_id,g.id,g.backend_epoch_id,
 g.generation_set_id,g.profile_digest,g.module_instance_id,g.channel,
 EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id),
 EXISTS(SELECT 1 FROM akos_withdraw_v1.index_chunk_deletion_fences f WHERE f.generation_id=g.id),
 count(p.chunk_id) OVER(PARTITION BY g.id),p.payload
 FROM akos_withdraw_v1.index_generations g
 LEFT JOIN akos_withdraw_v1.index_projection_rows p ON p.generation_id=g.id
 WHERE g.space_id=$1::text AND g.kb_id=ANY($2::text[]) AND g.id=ANY($3::text[])
 ORDER BY g.id COLLATE "C",p.chunk_id COLLATE "C" LIMIT 10001`, string(r.scope.SpaceID), queryKBs(r.scope.KnowledgeBaseIDs), queryKBs(ids))
	if err != nil {
		return ErrRepository
	}
	defer rows.Close()
	snapshots := map[contracts.ID]contracts.IndexSnapshot{}
	bytesByGeneration := map[contracts.ID]int{}
	remaining, count := 16<<20, 0
	for rows.Next() {
		var state contracts.IndexDeletionState
		var set, module contracts.ID
		var profile contracts.Digest
		var channel contracts.Channel
		var chunkDeleted bool
		var raw []byte
		if rows.Scan(&state.SpaceID, &state.KnowledgeBaseID, &state.DocumentID, &state.RevisionID, &state.GenerationID, &state.BackendEpochID,
			&set, &profile, &module, &channel, &state.Deleted, &chunkDeleted, &state.RecordCount, &raw) != nil {
			return ErrRepository
		}
		fact, ok := byGeneration[state.GenerationID]
		g := fact.GenerationSet.Channels[contracts.KeywordChannel]
		prepared := r.prepared[fact.DocumentID]
		if !ok || state.SpaceID != fact.SpaceID || state.KnowledgeBaseID != fact.KnowledgeBaseID || state.DocumentID != fact.DocumentID ||
			state.RevisionID != fact.RevisionID || state.BackendEpochID != fact.GenerationSet.BackendEpochID || set != fact.GenerationSet.ID ||
			profile != g.ProfileDigest || profile != prepared.artifact.Profile.Digest || state.BackendEpochID != prepared.plan.Backend.EpochID || module != g.BackendBinding.ModuleInstanceID || channel != contracts.KeywordChannel ||
			state.Deleted || chunkDeleted || state.RecordCount < 1 || state.RecordCount > 1000 {
			return ErrRepository
		}
		count++
		bytesByGeneration[g.ID] += len(raw)
		if count > 10000 || bytesByGeneration[g.ID] > 4<<20 {
			return ErrRepository
		}
		var record contracts.IndexProjection
		if json.Unmarshal(raw, &record) != nil {
			return ErrRepository
		}
		encoded, err := json.Marshal(record)
		if err != nil || len(encoded) > remaining {
			return ErrRepository
		}
		remaining -= len(encoded)
		snapshot := snapshots[g.ID]
		if snapshot.Format == "" {
			snapshot = contracts.IndexSnapshot{Format: indexsnapshot.Format, SpaceID: state.SpaceID, KnowledgeBaseID: state.KnowledgeBaseID, Generations: []contracts.IndexDeletionState{state}}
		}
		snapshot.Records = append(snapshot.Records, record)
		snapshots[g.ID] = snapshot
	}
	if rows.Err() != nil || len(snapshots) != len(facts) {
		return ErrRepository
	}
	for id, snapshot := range snapshots {
		if len(snapshot.Records) != snapshot.Generations[0].RecordCount {
			return ErrRepository
		}
		sealed, err := indexsnapshot.Seal(snapshot)
		prepared := r.prepared[byGeneration[id].DocumentID]
		if err != nil || sealed.Checksum != prepared.artifact.SnapshotDigest {
			return ErrRepository
		}
		if r.snapshots == nil {
			r.snapshots = map[contracts.ID]contracts.IndexSnapshot{}
		}
		r.snapshots[id] = sealed
	}
	return nil
}
