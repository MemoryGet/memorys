package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

// PublicationFinalizer is the default atomic control/index implementation. A
// remote index implementation must supply equivalent current-effect fencing.
type PublicationFinalizer struct {
	DB                   *sql.DB
	Verify               VerifyScope
	AuthorizePublication func(context.Context, preparation.PublicationRequest, document.PublicationStartPlan) error
	ModuleInstanceID     contracts.ID
}

var _ preparation.PublicationStore = PublicationFinalizer{}

func (p PublicationFinalizer) Finalize(ctx context.Context, input preparation.PublicationRequest) (preparation.PublicationResult, error) {
	result, err := p.finalize(ctx, input)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, ErrCommitUnknown) {
		return preparation.PublicationResult{}, errors.Join(preparation.ErrPublicationUnknown, err)
	}
	return preparation.PublicationResult{}, errors.Join(preparation.ErrPublicationRejected, err)
}
func (p PublicationFinalizer) finalize(ctx context.Context, input preparation.PublicationRequest) (preparation.PublicationResult, error) {
	var result preparation.PublicationResult
	if ctx == nil || p.DB == nil || p.Verify == nil || p.AuthorizePublication == nil || !commandID(string(p.ModuleInstanceID)) || !validActorCommand(input.Command, input.OperationID) || input.Command.Scope.Action != "publication.publish" || input.Command.Principal.SubjectID != input.Command.Scope.SubjectID || input.Command.Principal.SubjectType != "user" && input.Command.Principal.SubjectType != "service" || input.ExpectedEpoch == math.MaxUint64 || !commandDigest(input.ArtifactDigest) || input.Command.Request.Deadline.IsZero() {
		return result, ErrRepository
	}
	if _, ok := ctx.Deadline(); !ok {
		return result, ErrRepository
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 65536 {
		return result, ErrRepository
	}
	var r preparation.PublicationRequest
	if json.Unmarshal(raw, &r) != nil {
		return result, ErrRepository
	}
	c := r.Command
	actorDigest, digest, err := preparation.PublicationDigests(r)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithDeadline(ctx, c.Request.Deadline)
	defer cancel()
	var savedPlan []byte
	authorize := func(cx context.Context) error {
		if savedPlan == nil {
			return nil
		}
		var plan document.PublicationStartPlan
		var request preparation.PublicationRequest
		if json.Unmarshal(savedPlan, &plan) != nil || json.Unmarshal(raw, &request) != nil || p.AuthorizePublication(cx, request, plan) != nil || cx.Err() != nil {
			return ErrRepository
		}
		return nil
	}
	verify := func(cx context.Context, scope contracts.AccessScope) error {
		if p.Verify(cx, cloneScope(scope)) != nil || authorize(cx) != nil || cx.Err() != nil {
			return ErrRepository
		}
		return nil
	}
	err = withinSession(ctx, p.DB, c.Scope, verify, func(cx context.Context, s *session) error {
		if err := s.enter(cx, c.Scope); err != nil {
			return err
		}
		s.mu.Unlock()
		if s.tx.QueryRowContext(cx, `SELECT payload FROM akos_withdraw_v1.publication_intents WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text`, string(c.Scope.SpaceID), string(c.Scope.KnowledgeBaseIDs[0]), string(r.OperationID)).Scan(&savedPlan) != nil || len(savedPlan) > 262144 {
			return ErrRepository
		}
		var plan document.PublicationStartPlan
		if json.Unmarshal(savedPlan, &plan) != nil || plan.Result.Operation.ID != r.OperationID || plan.SpaceID != c.Scope.SpaceID || plan.KnowledgeBaseID != c.Scope.KnowledgeBaseIDs[0] || !reflect.DeepEqual(preparation.PublisherIdentity(c.Principal), plan.OriginalPrincipal) || c.Request.ProfileDigest != plan.ProfileDigest || authorize(cx) != nil {
			return ErrRepository
		}
		var oldDigest, oldActor string
		var receipt []byte
		err := s.tx.QueryRowContext(cx, `SELECT f.request_digest,o.actor_semantic_digest,f.receipt FROM akos_withdraw_v1.publication_finalizations f JOIN akos_withdraw_v1.command_operations o ON o.id=f.operation_id WHERE f.space_id=$1::text AND f.kb_id=$2::text AND f.operation_id=$3::text AND o.idempotency_key=$4::text AND o.action='publish'`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(r.OperationID), c.IdempotencyKey).Scan(&oldDigest, &oldActor, &receipt)
		if err == nil {
			if oldDigest != string(digest) || oldActor != string(actorDigest) || len(receipt) > 2<<20 || json.Unmarshal(receipt, &result) != nil || result.Operation.ID != r.OperationID || result.Operation.State != "committed" || result.History.CommandDigest != digest || result.History.PublicationEpoch != r.ExpectedEpoch+1 {
				return ErrRepository
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return ErrRepository
		}
		// Take the document lock first, matching review/withdrawal/config ordering.
		var doc contracts.Document
		var epoch, version string
		var oldRevision, oldSet sql.NullString
		var deleted bool
		if s.tx.QueryRowContext(cx, `SELECT id,space_id,kb_id,publication_epoch::text,row_version::text,active_revision_id,active_generation_set_id,deleted FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR UPDATE`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(plan.Result.Operation.DocumentID)).Scan(&doc.ID, &doc.SpaceID, &doc.KnowledgeBaseID, &epoch, &version, &oldRevision, &oldSet, &deleted) != nil || deleted {
			return ErrRepository
		}
		doc.PublicationEpoch, err = strconv.ParseUint(epoch, 10, 64)
		if err != nil {
			return err
		}
		doc.RowVersion, err = strconv.ParseUint(version, 10, 64)
		if err != nil {
			return err
		}
		doc.ActiveRevisionID = idPointer(oldRevision)
		doc.ActiveGenerationSetID = idPointer(oldSet)
		if err := lockPublicationTaskAuthority(cx, s.tx, plan); err != nil {
			return err
		}
		completed, err := inspectCompletedPublication(cx, s.tx, c.Scope, plan, r.ArtifactDigest, p.ModuleInstanceID)
		if err != nil {
			return err
		}
		var previous contracts.WithdrawalSnapshot
		if doc.ActiveRevisionID != nil {
			previous, err = s.loadWithdrawalCurrent(cx, c.Scope, doc.ID)
			if err != nil {
				return err
			}
		}
		var now time.Time
		if s.tx.QueryRowContext(cx, `SELECT clock_timestamp()`).Scan(&now) != nil {
			return ErrRepository
		}
		historyID, err := submissionID("publication-history-")
		if err != nil {
			return err
		}
		result, err = preparation.PublicationFacts(r, plan, completed, doc, historyID, digest, now.UTC())
		if err != nil {
			return err
		}
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.command_operations(id,space_id,kb_id,document_id,actor_semantic_digest,actor_id,action,idempotency_key,command_digest,state,history_id) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,'publish',$7::text,$8::text,'pending',NULL)`, string(r.OperationID), string(plan.SpaceID), string(plan.KnowledgeBaseID), string(doc.ID), string(actorDigest), string(c.Principal.SubjectID), c.IdempotencyKey, string(digest)); err != nil {
			return ErrRepository
		}
		if err := (historyRepo{s}).Append(cx, c.Scope, result.History); err != nil {
			return err
		}
		if doc.ActiveRevisionID != nil {
			planID, err := submissionID("replacement-cleanup-")
			if err != nil {
				return err
			}
			cleanup := contracts.CleanupPlan{ID: planID, HistoryID: historyID, OperationID: r.OperationID, SpaceID: plan.SpaceID, KnowledgeBaseID: plan.KnowledgeBaseID, DocumentID: doc.ID, RevisionID: previous.RevisionID, GenerationSetID: previous.GenerationSetID, PublicationEpoch: previous.PublicationEpoch, ProfileDigest: c.Request.ProfileDigest, Targets: previous.Targets, CreatedAt: result.History.OccurredAt}
			if err := (planRepo{s}).Append(cx, c.Scope, cleanup); err != nil {
				return err
			}
			result.CleanupPlanID = &planID
		}
		for _, g := range completed.Artifact.GenerationSet.Channels {
			definition, _ := json.Marshal(map[string]any{"generation_id": g.ID})
			if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.binding_definitions(space_id,kb_id,id,module_instance_id,profile_digest,kind,definition) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,'index',$6::jsonb)`, string(plan.SpaceID), string(plan.KnowledgeBaseID), "published-index-"+string(g.ID), string(g.BackendBinding.ModuleInstanceID), string(g.ProfileDigest), string(definition)); err != nil {
				return ErrRepository
			}
		}
		changed, err := s.tx.ExecContext(cx, `UPDATE akos_withdraw_v1.documents SET publication_epoch=$4::numeric,row_version=row_version+1,active_revision_id=$5::text,active_generation_set_id=$6::text WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text AND publication_epoch=$7::numeric AND row_version=$8::numeric AND NOT deleted`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(doc.ID), strconv.FormatUint(result.History.PublicationEpoch, 10), string(*result.History.RevisionID), string(*result.History.GenerationSetID), strconv.FormatUint(doc.PublicationEpoch, 10), strconv.FormatUint(doc.RowVersion, 10))
		if err != nil {
			return ErrRepository
		}
		if n, e := changed.RowsAffected(); e != nil || n != 1 {
			return ErrRepository
		}
		bytes, _ := json.Marshal(result)
		if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.publication_finalizations(space_id,kb_id,operation_id,job_id,document_id,history_id,artifact_digest,request_digest,receipt) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::text,$8::text,$9::jsonb)`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(r.OperationID), string(completed.Job.ID), string(doc.ID), string(historyID), string(r.ArtifactDigest), string(digest), string(bytes)); err != nil {
			return ErrRepository
		}
		for _, table := range []string{"publication_publish_audit", "publication_publish_outbox"} {
			if _, err := s.tx.ExecContext(cx, `INSERT INTO akos_withdraw_v1.`+table+`(space_id,kb_id,operation_id,history_id,request_id,actor_id,publication_epoch,cleanup_plan_id,payload) VALUES($1::text,$2::text,$3::text,$4::text,$5::text,$6::text,$7::numeric,$8::text,$9::jsonb)`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(r.OperationID), string(historyID), string(c.Request.RequestID), string(c.Principal.SubjectID), strconv.FormatUint(result.History.PublicationEpoch, 10), nullableID(result.CleanupPlanID), string(bytes)); err != nil {
				return ErrRepository
			}
		}
		changed, err = s.tx.ExecContext(cx, `UPDATE akos_withdraw_v1.command_operations SET state='committed',history_id=$2::text WHERE id=$1::text AND state='pending'`, string(r.OperationID), string(historyID))
		if err != nil {
			return ErrRepository
		}
		if n, e := changed.RowsAffected(); e != nil || n != 1 {
			return ErrRepository
		}
		return nil
	})
	if err != nil {
		return preparation.PublicationResult{}, err
	}
	return result, nil
}

// Reconstructing the historical running shape is solely for source projection
// comparison. It neither revives nor authorizes the completed execution lease.
func inspectCompletedPublication(ctx context.Context, tx *sql.Tx, scope contracts.AccessScope, plan document.PublicationStartPlan, digest contracts.Digest, module contracts.ID) (preparation.CompletionResult, error) {
	var completed preparation.CompletionResult
	var payload, artifactBytes []byte
	var worker, until string
	head, err := loadPublicationTaskHead(ctx, tx, scope, plan.Result.Job.ID, "FOR SHARE")
	if err != nil {
		return completed, err
	}
	if tx.QueryRowContext(ctx, `SELECT result,worker_id,lease_until FROM akos_withdraw_v1.publication_completions WHERE space_id=$1::text AND kb_id=$2::text AND job_id=$3::text AND operation_id=$4::text AND artifact_digest=$5::text`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(head.job.ID), string(plan.Result.Operation.ID), string(digest)).Scan(&payload, &worker, &until) != nil || len(payload) > 2<<20 || json.Unmarshal(payload, &completed) != nil || !reflect.DeepEqual(head.job, completed.Job) || head.job.State != "completed" {
		return preparation.CompletionResult{}, ErrRepository
	}
	a := completed.Artifact
	if tx.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.publication_prepared_artifacts WHERE space_id=$1::text AND kb_id=$2::text AND operation_id=$3::text AND attempt_id=$4::text AND artifact_digest=$5::text`, string(plan.SpaceID), string(plan.KnowledgeBaseID), string(a.OperationID), string(a.AttemptID), string(digest)).Scan(&artifactBytes) != nil {
		return preparation.CompletionResult{}, ErrRepository
	}
	var persisted preparation.Artifact
	if json.Unmarshal(artifactBytes, &persisted) != nil || !reflect.DeepEqual(persisted, a) || a.Profile.Bindings[contracts.IndexWriting].InstanceID != module {
		return preparation.CompletionResult{}, ErrRepository
	}
	actual, err := readPreparationSnapshot(ctx, tx, plan, a)
	if err != nil {
		return preparation.CompletionResult{}, err
	}
	expires, err := time.Parse(timeFormat, until)
	if err != nil {
		return preparation.CompletionResult{}, err
	}
	head.job.State = "running"
	head.job.RowVersion--
	head.job.Progress = 0
	head.job.GenerationID = nil
	head.job.LeaseUntil = &expires
	head.lease = contracts.Lease{JobID: head.job.ID, WorkerID: contracts.ID(worker), AttemptID: a.AttemptID, FencingToken: a.FencingToken, ExpiresAt: expires}
	work, err := readPublicationWork(ctx, tx, plan, head)
	if err != nil {
		return preparation.CompletionResult{}, err
	}
	chunks := make([]contracts.Chunk, 0, len(actual.Records))
	for _, record := range actual.Records {
		chunks = append(chunks, record.Chunk)
	}
	expected, reference, err := preparation.Build(work, a.Profile, chunks)
	if err != nil || preparation.MatchArtifact(a, expected) != nil || reference.Checksum != actual.Checksum {
		return preparation.CompletionResult{}, ErrRepository
	}
	return completed, nil
}
