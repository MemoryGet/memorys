package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/indexsnapshot"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

// PreparationIndex is a keyword preparation reference backend. Sharing the DB
// with the control source lets the lease lock fence the entire index write.
// It is not the full IndexWriter, retriever or publication service.
type PreparationIndex struct {
	Tasks            PublicationTasks
	ModuleInstanceID contracts.ID
}

var _ preparation.Backend = PreparationIndex{}

func (p PreparationIndex) owned(ctx context.Context, r document.PublicationReadWorkRequest, kind string, fn func(context.Context, *session, document.PublicationStartPlan) error) error {
	c := document.PublicationTaskAccess{Kind: kind, Request: r.Request, Scope: r.Scope, JobID: r.Lease.JobID, WorkerID: r.Lease.WorkerID, Lease: r.Lease}
	return p.Tasks.within(ctx, c, func(cx context.Context, s *session, plan document.PublicationStartPlan) error {
		if err := lockPublicationTaskAuthority(cx, s.tx, plan); err != nil {
			return err
		}
		head, err := loadPublicationTaskHead(cx, s.tx, c.Scope, c.JobID, "FOR SHARE")
		if err != nil {
			return err
		}
		var now time.Time
		if s.tx.QueryRowContext(cx, `SELECT clock_timestamp()`).Scan(&now) != nil || head.job.State != "running" || !document.SamePublicationLease(head.lease, c.Lease) || !c.Lease.ExpiresAt.After(now) {
			return document.ErrPublicationTaskRejected
		}
		return fn(cx, s, plan)
	})
}

func preparationError(err error) error {
	if errors.Is(err, ErrCommitUnknown) {
		return errors.Join(ErrCommitUnknown, preparation.ErrPreparationUnknown)
	}
	if errors.Is(err, document.ErrPublicationSuperseded) {
		return document.ErrPublicationSuperseded
	}
	return preparation.ErrPreparation
}

func (p PreparationIndex) PrepareOwned(ctx context.Context, input preparation.Request) (preparation.Artifact, error) {
	var empty preparation.Artifact
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 8<<20 {
		return empty, preparation.ErrPreparation
	}
	var r preparation.Request
	if json.Unmarshal(raw, &r) != nil {
		return empty, preparation.ErrPreparation
	}
	work, err := p.Tasks.ReadOwned(ctx, r.Access)
	if err != nil {
		return empty, err
	}
	artifact, expected, err := preparation.Build(work, r.Profile, r.Chunks)
	if err != nil {
		return empty, err
	}
	if p.ModuleInstanceID == "" || r.Profile.Bindings[contracts.IndexWriting].InstanceID != p.ModuleInstanceID {
		return empty, preparation.ErrPreparation
	}
	err = p.owned(ctx, r.Access, "prepare_index", func(cx context.Context, s *session, plan document.PublicationStartPlan) error {
		var existing []byte
		err := s.tx.QueryRowContext(cx, `SELECT payload FROM akos_withdraw_v1.publication_prepared_artifacts WHERE space_id=$1::text AND kb_id=$2::text AND operation_id=$3::text AND attempt_id=$4::text`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(artifact.OperationID), string(artifact.AttemptID)).Scan(&existing)
		if err == nil {
			var previous preparation.Artifact
			if json.Unmarshal(existing, &previous) != nil || !reflect.DeepEqual(previous, artifact) {
				return ErrRepository
			}
			actual, err := readPreparationSnapshot(cx, s.tx, plan, artifact)
			if err != nil || actual.Checksum != expected.Checksum {
				return ErrRepository
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return ErrRepository
		}
		set := artifact.GenerationSet
		generation := set.Channels[contracts.KeywordChannel]
		args := []any{string(plan.SpaceID), string(plan.KnowledgeBaseID), string(plan.Result.Operation.DocumentID), string(plan.Result.Operation.RevisionID)}
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.generation_sets(space_id,kb_id,document_id,revision_id,id,backend_epoch_id) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text)`, append(args, string(set.ID), string(set.BackendEpochID))...); err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.index_generations(space_id,kb_id,document_id,revision_id,generation_set_id,id,backend_epoch_id,channel,profile_digest,module_instance_id) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,'keyword',$8::text,$9::text)`, append(args, string(set.ID), string(generation.ID), string(set.BackendEpochID), string(r.Profile.Digest), string(p.ModuleInstanceID))...); err != nil {
			return ErrRepository
		}
		for _, record := range expected.Records {
			payload, err := json.Marshal(record)
			if err != nil {
				return ErrRepository
			}
			if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.index_projection_rows(generation_id,chunk_id,payload) VALUES($1::text,$2::text,$3::jsonb)`, string(record.GenerationID), string(record.Chunk.ID), string(payload)); err != nil {
				return ErrRepository
			}
		}
		actual, err := readPreparationSnapshot(cx, s.tx, plan, artifact)
		if err != nil || actual.Checksum != expected.Checksum {
			return ErrRepository
		}
		artifactBytes, err := json.Marshal(artifact)
		if err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_prepared_artifacts(space_id,kb_id,operation_id,job_id,attempt_id,fencing_token,generation_set_id,generation_id,artifact_digest,snapshot_digest,payload) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::numeric,$7::text,$8::text,$9::text,$10::text,$11::jsonb)`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(artifact.OperationID), string(artifact.JobID), string(artifact.AttemptID), artifact.FencingToken, string(set.ID), string(generation.ID), string(artifact.Digest), string(artifact.SnapshotDigest), string(artifactBytes)); err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_preparation_audit(space_id,kb_id,operation_id,attempt_id,worker_id,request_id) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text)`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(artifact.OperationID), string(artifact.AttemptID), string(r.Access.Lease.WorkerID), string(r.Access.Request.RequestID)); err != nil {
			return ErrRepository
		}
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_preparation_outbox(space_id,kb_id,operation_id,attempt_id,artifact_digest) VALUES($1::text,$2::text,$3::text,$4::text,$5::text)`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(artifact.OperationID), string(artifact.AttemptID), string(artifact.Digest)); err != nil {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return empty, preparationError(err)
	}
	return artifact, nil
}

// This read path independently reloads actual rows and tests scoped keyword
// visibility. It does not trust an adapter's echoed ReadinessProof counts.
func (p PreparationIndex) InspectOwned(ctx context.Context, r document.PublicationReadWorkRequest, input preparation.Artifact) (contracts.IndexSnapshot, error) {
	var snapshot contracts.IndexSnapshot
	work, err := p.Tasks.ReadOwned(ctx, r)
	if err != nil {
		return snapshot, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return snapshot, preparation.ErrPreparation
	}
	var artifact preparation.Artifact
	if json.Unmarshal(raw, &artifact) != nil {
		return snapshot, preparation.ErrPreparation
	}
	if artifact.JobID != work.Job.ID || artifact.AttemptID != r.Lease.AttemptID || artifact.FencingToken != r.Lease.FencingToken || artifact.OperationID != work.Job.OperationID || artifact.Profile.Bindings[contracts.IndexWriting].InstanceID != p.ModuleInstanceID {
		return snapshot, preparation.ErrPreparation
	}
	err = p.owned(ctx, r, "inspect_index", func(cx context.Context, s *session, plan document.PublicationStartPlan) error {
		var payload []byte
		var stored preparation.Artifact
		if s.tx.QueryRowContext(cx, `SELECT payload FROM akos_withdraw_v1.publication_prepared_artifacts WHERE space_id=$1::text AND kb_id=$2::text AND operation_id=$3::text AND attempt_id=$4::text`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(artifact.OperationID), string(artifact.AttemptID)).Scan(&payload) != nil || json.Unmarshal(payload, &stored) != nil || !reflect.DeepEqual(stored, artifact) {
			return ErrRepository
		}
		actual, err := readPreparationSnapshot(cx, s.tx, plan, artifact)
		if err != nil {
			return err
		}
		chunks := make([]contracts.Chunk, 0, len(actual.Records))
		for _, record := range actual.Records {
			chunks = append(chunks, record.Chunk)
		}
		expected, reference, err := preparation.Build(work, artifact.Profile, chunks)
		if err != nil || preparation.MatchArtifact(artifact, expected) != nil || actual.Checksum != reference.Checksum {
			return ErrRepository
		}
		snapshot = actual
		return nil
	})
	if err != nil {
		return contracts.IndexSnapshot{}, preparationError(err)
	}
	return snapshot, nil
}

func readPreparationSnapshot(ctx context.Context, tx *sql.Tx, plan document.PublicationStartPlan, artifact preparation.Artifact) (contracts.IndexSnapshot, error) {
	var snapshot contracts.IndexSnapshot
	generation := artifact.GenerationSet.Channels[contracts.KeywordChannel]
	if generation.ID == "" || generation.BackendBinding == nil {
		return snapshot, ErrRepository
	}
	var state contracts.IndexDeletionState
	err := tx.QueryRowContext(ctx, `SELECT g.space_id,g.kb_id,g.document_id,g.revision_id,g.id,g.backend_epoch_id,EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id),(SELECT count(*) FROM akos_withdraw_v1.index_projection_rows r WHERE r.generation_id=g.id) FROM akos_withdraw_v1.index_generations g WHERE g.space_id=$1::text AND g.kb_id=$2::text AND g.document_id=$3::text AND g.revision_id=$4::text AND g.id=$5::text AND g.generation_set_id=$6::text AND g.backend_epoch_id=$7::text AND g.profile_digest=$8::text AND g.module_instance_id=$9::text AND g.channel='keyword' FOR SHARE OF g`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(plan.Result.Operation.DocumentID), string(plan.Result.Operation.RevisionID), string(generation.ID), string(artifact.GenerationSet.ID), string(plan.Backend.EpochID), string(artifact.Profile.Digest), string(generation.BackendBinding.ModuleInstanceID)).Scan(&state.SpaceID, &state.KnowledgeBaseID, &state.DocumentID, &state.RevisionID, &state.GenerationID, &state.BackendEpochID, &state.Deleted, &state.RecordCount)
	if err != nil || state.Deleted || state.RecordCount < 1 || state.RecordCount > 1000 {
		return snapshot, ErrRepository
	}
	snapshot = contracts.IndexSnapshot{Format: indexsnapshot.Format, SpaceID: plan.SpaceID, KnowledgeBaseID: plan.KnowledgeBaseID, Generations: []contracts.IndexDeletionState{state}}
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM akos_withdraw_v1.index_projection_rows WHERE generation_id=$1::text ORDER BY chunk_id LIMIT 1001`, string(generation.ID))
	if err != nil {
		return snapshot, ErrRepository
	}
	for rows.Next() {
		var raw []byte
		var record contracts.IndexProjection
		if rows.Scan(&raw) != nil || len(raw) > 4<<20 || json.Unmarshal(raw, &record) != nil {
			rows.Close()
			return contracts.IndexSnapshot{}, ErrRepository
		}
		snapshot.Records = append(snapshot.Records, record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(snapshot.Records) != state.RecordCount {
		return contracts.IndexSnapshot{}, ErrRepository
	}
	for _, record := range snapshot.Records {
		query := `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_projection_rows r JOIN akos_withdraw_v1.index_generations g ON g.id=r.generation_id WHERE g.space_id=$1::text AND g.kb_id=$2::text AND g.revision_id=$3::text AND g.backend_epoch_id=$4::text AND g.id=$5::text AND r.chunk_id=$6::text AND (r.keyword_vector @@ plainto_tsquery('simple'::regconfig,$7::text) OR strpos(r.payload->'Chunk'->>'Text',$7::text)>0))`
		args := []any{string(plan.SpaceID), string(plan.KnowledgeBaseID), string(plan.Result.Operation.RevisionID), string(plan.Backend.EpochID), string(generation.ID), string(record.Chunk.ID), record.Chunk.Text}
		var visible bool
		if tx.QueryRowContext(ctx, query, args...).Scan(&visible) != nil || !visible {
			return contracts.IndexSnapshot{}, ErrRepository
		}
		for _, field := range []int{0, 1, 2, 3, 4} {
			negative := append([]any(nil), args...)
			negative[field] = "other-scope-or-generation"
			if tx.QueryRowContext(ctx, query, negative...).Scan(&visible) != nil || visible {
				return contracts.IndexSnapshot{}, ErrRepository
			}
		}
	}
	return indexsnapshot.Seal(snapshot)
}
