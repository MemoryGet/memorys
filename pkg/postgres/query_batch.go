package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/preparation"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// prefetch reads exactly these platform IDs in the existing Repeatable Read
// transaction. All original per-document relationships/digests are still
// checked below; this only combines database round trips, never read views.
func (r *queryRepository) prefetch(ctx context.Context, ids []contracts.ID) error {
	if len(ids) < 1 || len(ids) > 1000 {
		return queryread.ErrInvalid
	}
	missing := []contracts.ID{}
	wanted := map[contracts.ID]bool{}
	for _, id := range ids {
		if !commandID(string(id)) || wanted[id] {
			return queryread.ErrInvalid
		}
		wanted[id] = true
		if _, ok := r.states[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return ctx.Err()
	}
	rows, err := r.tx.QueryContext(ctx, `SELECT d.id,d.space_id,d.kb_id,d.publication_epoch::text,d.row_version::text,
 d.active_revision_id,d.active_generation_set_id,d.deleted,bh.backend_epoch_id,be.payload,i.payload,a.payload,
 ar.payload,ar.current_decision_id,rp.payload,rp.digest
 FROM akos_withdraw_v1.documents d
 LEFT JOIN akos_withdraw_v1.publication_backend_heads bh ON bh.space_id=d.space_id AND bh.kb_id=d.kb_id
 LEFT JOIN akos_withdraw_v1.publication_backend_epochs be ON be.space_id=bh.space_id AND be.kb_id=bh.kb_id AND be.id=bh.backend_epoch_id
 LEFT JOIN (akos_withdraw_v1.publication_finalizations f
 JOIN akos_withdraw_v1.publication_history h ON h.id=f.history_id
 JOIN akos_withdraw_v1.publication_intents i ON i.id=f.operation_id
 JOIN akos_withdraw_v1.publication_prepared_artifacts a ON a.operation_id=i.id AND a.artifact_digest=f.artifact_digest)
 ON f.space_id=d.space_id AND f.kb_id=d.kb_id AND f.document_id=d.id
 AND h.publication_epoch=d.publication_epoch AND h.revision_id=d.active_revision_id AND h.generation_set_id=d.active_generation_set_id
 LEFT JOIN akos_withdraw_v1.approval_requests ar ON ar.space_id=d.space_id AND ar.kb_id=d.kb_id AND ar.document_id=d.id AND ar.revision_id=d.active_revision_id
 LEFT JOIN akos_withdraw_v1.review_policy_heads rh ON rh.space_id=d.space_id AND rh.kb_id=d.kb_id
 LEFT JOIN akos_withdraw_v1.review_policy_snapshots rp ON rp.space_id=rh.space_id AND rp.kb_id=rh.kb_id AND rp.version=rh.policy_version
 WHERE d.space_id=$1::text AND d.kb_id=ANY($2::text[]) AND d.id=ANY($3::text[]) LIMIT 1001`, string(r.scope.SpaceID), queryKBs(r.scope.KnowledgeBaseIDs), queryKBs(missing))
	if err != nil {
		return queryread.ErrRead
	}
	defer rows.Close()
	seen := map[contracts.ID]bool{}
	for rows.Next() {
		var state contracts.CurrentDocumentState
		var epoch, version string
		var revision, set, backendID, current, policyDigest sql.NullString
		var deleted bool
		var backendRaw, planRaw, artifactRaw, requestRaw, policyRaw []byte
		if rows.Scan(&state.Document.ID, &state.Document.SpaceID, &state.Document.KnowledgeBaseID, &epoch, &version,
			&revision, &set, &deleted, &backendID, &backendRaw, &planRaw, &artifactRaw, &requestRaw, &current, &policyRaw, &policyDigest) != nil {
			return queryread.ErrRead
		}
		id := state.Document.ID
		if !wanted[id] || seen[id] || deleted || !r.allowed(state.Document.KnowledgeBaseID) || state.Document.SpaceID != r.scope.SpaceID {
			return queryread.ErrRead
		}
		seen[id] = true
		state.Document.PublicationEpoch, err = strconv.ParseUint(epoch, 10, 64)
		if err != nil {
			return queryread.ErrRead
		}
		state.Document.RowVersion, err = strconv.ParseUint(version, 10, 64)
		if err != nil || state.Document.RowVersion == 0 {
			return queryread.ErrRead
		}
		state.Document.ActiveRevisionID = idPointer(revision)
		state.Document.ActiveGenerationSetID = idPointer(set)
		var backend document.PublicationBackendSnapshot
		if !backendID.Valid || len(backendRaw) > 65536 || json.Unmarshal(backendRaw, &backend) != nil || string(backend.EpochID) != backendID.String ||
			backend.Number == 0 || !commandID(backend.CutoverWatermark) || !commandDigest(backend.ProfileDigest) {
			return queryread.ErrRead
		}
		state.ActiveBackendEpochID = backend.EpochID
		if revision.Valid && set.Valid && len(planRaw) > 0 && len(artifactRaw) > 0 {
			var plan document.PublicationStartPlan
			var artifact preparation.Artifact
			if len(planRaw) > 262144 || len(artifactRaw) > 1<<20 || json.Unmarshal(planRaw, &plan) != nil || json.Unmarshal(artifactRaw, &artifact) != nil ||
				plan.SpaceID != state.Document.SpaceID || plan.KnowledgeBaseID != state.Document.KnowledgeBaseID ||
				plan.Result.Operation.DocumentID != id || plan.Result.Operation.RevisionID != contracts.ID(revision.String) ||
				artifact.GenerationSet.ID != contracts.ID(set.String) || artifact.GenerationSet.RevisionID != contracts.ID(revision.String) ||
				artifact.OperationID != plan.Result.Operation.ID {
				return queryread.ErrRead
			}
			if digest, err := r.artifactDigest(artifact); err != nil || digest != artifact.Digest {
				return queryread.ErrRead
			}
			if !policyDigest.Valid {
				return queryread.ErrRead
			}
			review, err := currentReviewBytes(requestRaw, policyRaw, current, policyDigest.String, plan.Result.Operation.ApprovalID, plan.Review)
			if err != nil {
				return err
			}
			state.GenerationSet = &artifact.GenerationSet
			state.PublicationReview = &review
			if r.prepared == nil {
				r.prepared = map[contracts.ID]preparedQueryArtifact{}
			}
			r.prepared[id] = preparedQueryArtifact{plan: plan, artifact: artifact}
		}
		if err = r.takeState(id, state); err != nil {
			return err
		}
	}
	if rows.Err() != nil || len(seen) != len(missing) {
		return queryread.ErrRead
	}
	return nil
}

func (r *queryRepository) artifactDigest(artifact preparation.Artifact) (contracts.Digest, error) {
	if r.artifactDigests == nil {
		return preparation.ArtifactDigest(artifact)
	}
	return r.artifactDigests.Digest(artifact)
}

func (r *queryRepository) LoadCurrentDocuments(ctx context.Context, ids []contracts.ID) ([]contracts.CurrentDocumentState, error) {
	if r.verify(ctx) != nil {
		return nil, queryread.ErrRead
	}
	if err := r.prefetch(ctx, ids); err != nil {
		return nil, err
	}
	out := make([]contracts.CurrentDocumentState, 0, len(ids))
	for _, id := range ids {
		s, err := r.load(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if r.verify(ctx) != nil {
		return nil, queryread.ErrRead
	}
	return out, nil
}
