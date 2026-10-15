package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

// PublicationCompletion commits success only after reloading actual preparation
// data under the same control/lease/generation locks. It is a composite default
// adapter, not the full TaskRepository or final publication transaction.
type PublicationCompletion struct {
	Tasks            PublicationTasks
	ModuleInstanceID contracts.ID
}

var _ preparation.CompletionStore = PublicationCompletion{}

func (p PublicationCompletion) Complete(ctx context.Context, input preparation.CompletionRequest) (preparation.CompletionResult, error) {
	var result preparation.CompletionResult
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 65536 {
		return result, document.ErrPublicationTaskRejected
	}
	var r preparation.CompletionRequest
	if json.Unmarshal(raw, &r) != nil || !commandID(string(r.CommandID)) || !commandDigest(r.ArtifactDigest) || r.ExpectedJobVersion == 0 || !commandID(string(p.ModuleInstanceID)) {
		return result, document.ErrPublicationTaskRejected
	}
	r.Access.Lease.ExpiresAt = r.Access.Lease.ExpiresAt.UTC()
	c := document.PublicationTaskAccess{Kind: "complete", Request: r.Access.Request, Scope: r.Access.Scope, JobID: r.Access.Lease.JobID, WorkerID: r.Access.Lease.WorkerID, Lease: r.Access.Lease, CommandID: r.CommandID}
	c, err = copyPublicationTaskAccess(c)
	if err != nil {
		return result, err
	}
	semantic, _ := json.Marshal(struct {
		Job, Worker       contracts.ID
		Lease             contracts.Lease
		Profile, Artifact contracts.Digest
		Expected          uint64
	}{c.JobID, c.WorkerID, c.Lease, c.Request.ProfileDigest, r.ArtifactDigest, r.ExpectedJobVersion})
	sum := sha256.Sum256(append([]byte("akos.publication-completion/1\n"), semantic...))
	digest := hex.EncodeToString(sum[:])
	fresh := false
	err = p.Tasks.withinChecked(ctx, c, func(cx context.Context, s *session, plan document.PublicationStartPlan) error {
		var storedDigest string
		var receipt []byte
		err := s.tx.QueryRowContext(cx, `SELECT request_digest,result FROM akos_withdraw_v1.publication_completions WHERE space_id=$1::text AND worker_id=$2::text AND command_id=$3::text`, string(c.Scope.SpaceID), string(c.WorkerID), string(c.CommandID)).Scan(&storedDigest, &receipt)
		if err == nil {
			if storedDigest != digest || len(receipt) > 2<<20 || json.Unmarshal(receipt, &result) != nil || result.Job.ID != c.JobID || result.Job.State != "completed" || result.Job.RowVersion != r.ExpectedJobVersion+1 || result.Artifact.Digest != r.ArtifactDigest || result.CompletedAt.IsZero() {
				return ErrRepository
			}
			// This is a historical response. Do not require an expired execution
			// lease or treat its old readiness proof as current publication authority.
			return nil
		}
		if err != sql.ErrNoRows {
			return ErrRepository
		}
		if err := lockPublicationTaskAuthority(cx, s.tx, plan); err != nil {
			return err
		}
		head, err := loadPublicationTaskHead(cx, s.tx, c.Scope, c.JobID, "FOR UPDATE")
		if err != nil {
			return err
		}
		var now time.Time
		if s.tx.QueryRowContext(cx, `SELECT clock_timestamp()`).Scan(&now) != nil || head.job.State != "running" || !document.SamePublicationLease(head.lease, c.Lease) || !c.Lease.ExpiresAt.After(now) {
			return document.ErrPublicationTaskRejected
		}
		live, cancel := context.WithDeadline(cx, c.Lease.ExpiresAt)
		defer cancel()
		work, err := readPublicationWork(live, s.tx, plan, head)
		if err != nil {
			return err
		}
		var artifactBytes []byte
		var artifact preparation.Artifact
		if s.tx.QueryRowContext(live, `SELECT payload FROM akos_withdraw_v1.publication_prepared_artifacts WHERE space_id=$1::text AND kb_id=$2::text AND operation_id=$3::text AND job_id=$4::text AND attempt_id=$5::text AND artifact_digest=$6::text`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(head.job.OperationID), string(c.JobID), string(c.Lease.AttemptID), string(r.ArtifactDigest)).Scan(&artifactBytes) != nil || len(artifactBytes) > 1<<20 || json.Unmarshal(artifactBytes, &artifact) != nil || artifact.Profile.Bindings[contracts.IndexWriting].InstanceID != p.ModuleInstanceID {
			return ErrRepository
		}
		actual, err := readPreparationSnapshot(live, s.tx, plan, artifact)
		if err != nil {
			return err
		}
		chunks := make([]contracts.Chunk, 0, len(actual.Records))
		for _, record := range actual.Records {
			chunks = append(chunks, record.Chunk)
		}
		expected, reference, err := preparation.Build(work, artifact.Profile, chunks)
		if err != nil || preparation.MatchArtifact(artifact, expected) != nil || reference.Checksum != actual.Checksum || artifact.SnapshotDigest != actual.Checksum {
			return ErrRepository
		}
		job, err := preparation.CompleteJob(head.job, head.lease, r, artifact, now.UTC())
		if err != nil {
			return err
		}
		result = preparation.CompletionResult{Job: job, Artifact: artifact, CompletedAt: now.UTC()}
		jobBytes, _ := json.Marshal(job)
		resultBytes, _ := json.Marshal(result)
		changed, err := s.tx.ExecContext(live, `UPDATE akos_withdraw_v1.publication_task_heads SET state='completed',row_version=$4::numeric,worker_id=NULL,attempt_id=NULL,lease_until=NULL,payload=$5::jsonb WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text AND row_version=$6::numeric`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(c.JobID), strconv.FormatUint(job.RowVersion, 10), string(jobBytes), strconv.FormatUint(head.job.RowVersion, 10))
		if err != nil {
			return ErrRepository
		}
		if n, e := changed.RowsAffected(); e != nil || n != 1 {
			return ErrRepository
		}
		args := []any{string(plan.SpaceID), string(plan.KnowledgeBaseID), string(c.JobID), string(c.WorkerID), string(c.CommandID), string(c.Request.RequestID), digest, string(artifact.OperationID), string(artifact.AttemptID), strconv.FormatUint(artifact.FencingToken, 10), string(artifact.Digest), strconv.FormatUint(job.RowVersion, 10), c.Lease.ExpiresAt.Format(timeFormat), result.CompletedAt.Format(timeFormat), string(resultBytes)}
		if _, err := s.tx.ExecContext(live, `INSERT INTO akos_withdraw_v1.publication_completions(space_id,kb_id,job_id,worker_id,command_id,request_id,request_digest,operation_id,attempt_id,fencing_token,artifact_digest,job_version,lease_until,completed_at,result) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::text,$9::text,$10::numeric,$11::text,$12::numeric,$13::text,$14::text,$15::jsonb)`, args...); err != nil {
			return ErrRepository
		}
		for _, table := range []string{"publication_completion_audit", "publication_completion_outbox"} {
			if _, err := s.tx.ExecContext(live, `INSERT INTO akos_withdraw_v1.`+table+`(space_id,kb_id,job_id,worker_id,command_id,artifact_digest,payload) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::jsonb)`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(c.JobID), string(c.WorkerID), string(c.CommandID), string(artifact.Digest), string(jobBytes)); err != nil {
				return ErrRepository
			}
		}
		fresh = true
		return nil
	}, func(cx context.Context, tx *sql.Tx) error {
		if !fresh {
			return nil
		}
		var now time.Time
		if tx.QueryRowContext(cx, `SELECT clock_timestamp()`).Scan(&now) != nil || !c.Lease.ExpiresAt.After(now) {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return preparation.CompletionResult{}, taskMutationError(err)
	}
	// Keep the response independent of any driver buffers or callback aliases.
	raw, _ = json.Marshal(result)
	var owned preparation.CompletionResult
	if json.Unmarshal(raw, &owned) != nil || !reflect.DeepEqual(owned, result) {
		return preparation.CompletionResult{}, document.ErrPublicationTaskUnknown
	}
	return owned, nil
}
