package localcli

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/chunk"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

// The hook is internal fault injection for integration tests, never a CLI flag.
type publicationHook func(string) error

func checkpoint(h publicationHook, stage string) error {
	if h != nil {
		return h(stage)
	}
	return nil
}
func stageError(stage string) error {
	return errors.New("publication stopped at " + stage + "; retry the same draft and expected hash to reconcile")
}

func (s *state) publish(ctx context.Context, db *sql.DB, owner *authority, id contracts.ID, expected contracts.Digest, hook publicationHook) (preparation.PublicationResult, error) {
	var empty preparation.PublicationResult
	if owner.principal.SubjectType != "user" || owner.principal.SubjectID != s.m.Owner {
		return empty, errAuthority
	}
	if _, err := owner.scope(ctx, "document.submit", ""); err != nil {
		return empty, errAuthority
	}
	draft, err := s.loadDraft(ctx, db, id)
	if err != nil {
		return empty, err
	}
	frozen, err := document.BuildFrozenContent(draft)
	if err != nil || expected == "" || frozen.ContentHash != expected {
		return empty, errors.New("expected hash does not match the stored draft; inspect the import preview before publishing")
	}
	doc := draft.DocumentID
	key := func(stage string) string {
		return "local-" + digest([]byte(string(s.m.Installation)+"/"+string(id)+"/"+stage))
	}
	command, err := owner.command(ctx, "document.submit", doc, key("submit"))
	if err != nil {
		return empty, errAuthority
	}
	submissions := postgres.Submissions{DB: db, Verify: owner.verify, ProfileDigest: s.m.ProfileDigest, ReviewPolicyVersion: 1,
		AuthorizeSubmission: func(c context.Context, q document.SubmissionCommand) error {
			if q.DocumentID != doc || q.Request.DraftID != draft.ID || q.Request.ExpectedVersion != 1 {
				return errAuthority
			}
			return owner.checkCommand(c, q.Request.Command, "document.submit", doc)
		}, VerifyContent: func(c context.Context, scope contracts.AccessScope, d contracts.Draft) error {
			if owner.verify(c, scope) != nil || !reflect.DeepEqual(d, draft) {
				return errAuthority
			}
			if len(d.Blobs) > 0 {
				return s.verifyOriginalDraft(c, owner, d)
			}
			return nil
		}}
	submitted, err := submissions.SubmitDraft(ctx, document.SubmissionCommand{DocumentID: doc, Request: contracts.SubmitRequest{Command: command, DraftID: id, ExpectedVersion: 1}})
	if err != nil {
		return empty, stageError("submit")
	}
	if submitted.Revision.ContentHash != expected {
		return empty, stageError("submitted-content")
	}
	if err = checkpoint(hook, "submitted"); err != nil {
		return empty, err
	}
	command, err = owner.command(ctx, "revision.approve", doc, key("approve"))
	if err != nil {
		return empty, errAuthority
	}
	reviews := postgres.Reviews{DB: db, Verify: owner.verify, ProfileDigest: s.m.ProfileDigest, ReviewPolicyVersion: 1, AuthorizeDecision: func(c context.Context, q document.ReviewCommand) error {
		if q.DocumentID != doc || q.Request.RevisionID != submitted.Revision.ID || q.Request.Decision != "approve" || q.Request.ExpectedApprovalVersion != 0 || q.Request.Reason != "Explicit local owner publication of "+string(expected) {
			return errAuthority
		}
		return owner.checkCommand(c, q.Request.Command, "revision.approve", doc)
	}}
	approval, err := reviews.DecideReview(ctx, document.ReviewCommand{DocumentID: doc, Request: contracts.ApprovalDecisionRequest{Command: command, RevisionID: submitted.Revision.ID, Decision: "approve", Reason: "Explicit local owner publication of " + string(expected)}})
	if err != nil {
		return empty, stageError("approve")
	}
	if err = checkpoint(hook, "approved"); err != nil {
		return empty, err
	}
	// Replays also re-evaluate the current decision, owner and document. Old
	// receipts must not claim success after revocation, withdrawal or deletion.
	currentApproval := func(c context.Context) error {
		var ok bool
		err := db.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.approval_requests q JOIN akos_withdraw_v1.documents d ON d.id=q.document_id JOIN akos_withdraw_v1.review_policy_heads h ON h.space_id=q.space_id AND h.kb_id=q.kb_id JOIN akos_withdraw_v1.publication_backend_heads b ON b.space_id=q.space_id AND b.kb_id=q.kb_id WHERE q.space_id=$1 AND q.kb_id=$2 AND q.document_id=$3 AND q.revision_id=$4 AND q.state='approved' AND q.row_version=1 AND q.current_decision_id=$5 AND h.policy_version=1 AND h.row_version=1 AND b.backend_epoch_id='local-backend' AND b.row_version=1 AND NOT d.deleted AND ((d.publication_epoch=0 AND d.active_revision_id IS NULL) OR (d.publication_epoch=1 AND d.active_revision_id=$4)))`, s.m.Space, s.m.KB, doc, submitted.Revision.ID, approval.ID).Scan(&ok)
		if err != nil || !ok {
			return errAuthority
		}
		return nil
	}
	command, err = owner.command(ctx, "publication.start", doc, key("start"))
	if err != nil {
		return empty, errAuthority
	}
	starts := postgres.PublicationStarts{DB: db, Verify: owner.verify, ProfileDigest: s.m.ProfileDigest, AuthorizeStart: func(c context.Context, q contracts.PublishRequest) error {
		if q.DocumentID != doc || q.RevisionID != submitted.Revision.ID || q.ApprovalID != approval.ID || q.ExpectedEpoch != 0 || currentApproval(c) != nil {
			return errAuthority
		}
		return owner.checkCommand(c, q.Command, "publication.start", doc)
	}}
	// New content always creates a new document at epoch zero. Recovering a
	// started operation reuses that original epoch, never the current head.
	started, err := starts.StartPublication(ctx, contracts.PublishRequest{Command: command, DocumentID: doc, RevisionID: submitted.Revision.ID, ApprovalID: approval.ID, ExpectedEpoch: 0})
	if err != nil {
		return empty, stageError("start")
	}
	if err = checkpoint(hook, "started"); err != nil {
		return empty, err
	}
	publisher := func(c context.Context, p document.PublicationStartPlan) error {
		if !reflect.DeepEqual(p.OriginalPrincipal, owner.principal) || p.Result.Operation.ID != started.Operation.ID || p.Result.Operation.DocumentID != doc || p.ProfileDigest != s.m.ProfileDigest || p.Backend.ProfileDigest != s.profile.Index.Digest || currentApproval(c) != nil {
			return errAuthority
		}
		if _, err := owner.scope(c, "publication.start", doc); err != nil {
			return errAuthority
		}
		_, err := owner.scope(c, "publication.publish", doc)
		return err
	}
	artifact, err := s.prepare(ctx, db, started, owner, publisher, hook)
	if err != nil {
		return empty, err
	}
	command, err = owner.command(ctx, "publication.publish", doc, key("publish"))
	if err != nil {
		return empty, errAuthority
	}
	finalizer := postgres.PublicationFinalizer{DB: db, Verify: owner.verify, ModuleInstanceID: s.profile.Index.Bindings[contracts.IndexWriting].InstanceID, AuthorizePublication: func(c context.Context, q preparation.PublicationRequest, p document.PublicationStartPlan) error {
		if q.OperationID != started.Operation.ID || q.ExpectedEpoch != 0 || q.ArtifactDigest != artifact.Digest || publisher(c, p) != nil {
			return errAuthority
		}
		return owner.checkCommand(c, q.Command, "publication.publish", doc)
	}}
	result, err := finalizer.Finalize(ctx, preparation.PublicationRequest{Command: command, OperationID: started.Operation.ID, ExpectedEpoch: 0, ArtifactDigest: artifact.Digest})
	if err != nil {
		return empty, stageError("finalize")
	}
	return result, nil
}

func (s *state) prepare(ctx context.Context, db *sql.DB, started document.PublicationStartResult, owner *authority, publisher func(context.Context, document.PublicationStartPlan) error, hook publicationHook) (preparation.Artifact, error) {
	var empty preparation.Artifact
	worker, err := s.identity(ctx, db, true)
	if err != nil {
		return empty, err
	}
	defer worker.close()
	scope, err := worker.scope(ctx, "publication.prepare", started.Operation.DocumentID)
	if err != nil {
		return empty, errAuthority
	}
	// Serialize orchestrators for this job across processes. This dedicated
	// connection is released (and its session lock dropped) on process death.
	conn, err := db.Conn(ctx)
	if err != nil {
		return empty, stageError("lock")
	}
	defer conn.Close()
	locked := false
	if conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", string(started.Job.ID)).Scan(&locked) != nil || !locked {
		return empty, errors.New("publication already running; retry the same draft later")
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(c, "SELECT pg_advisory_unlock(hashtext($1))", string(started.Job.ID)); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	tasks := postgres.PublicationTasks{DB: db, Verify: worker.verify, AuthorizePublisher: publisher, AuthorizeTask: func(c context.Context, q document.PublicationTaskAccess) error {
		if q.JobID != started.Job.ID || q.WorkerID != s.m.Worker || q.Scope.Action != "publication.prepare" || q.Request.ProfileDigest != s.profile.Index.Digest {
			return errAuthority
		}
		return worker.verify(c, q.Scope)
	}}
	var job contracts.Job
	var raw []byte
	var workerID, attemptID, until sql.NullString
	if db.QueryRowContext(ctx, `SELECT payload,worker_id,attempt_id,lease_until FROM akos_withdraw_v1.publication_task_heads WHERE space_id=$1 AND kb_id=$2 AND id=$3`, s.m.Space, s.m.KB, started.Job.ID).Scan(&raw, &workerID, &attemptID, &until) != nil || json.Unmarshal(raw, &job) != nil {
		return empty, stageError("task-state")
	}
	if job.State == "completed" {
		if _, err := worker.scope(ctx, "publication.prepare", started.Operation.DocumentID); err != nil {
			return empty, errAuthority
		}
		var result preparation.CompletionResult
		if db.QueryRowContext(ctx, `SELECT result FROM akos_withdraw_v1.publication_completions WHERE space_id=$1 AND kb_id=$2 AND job_id=$3`, s.m.Space, s.m.KB, job.ID).Scan(&raw) != nil || json.Unmarshal(raw, &result) != nil || result.Job.ID != job.ID || result.Artifact.OperationID != started.Operation.ID {
			return empty, stageError("completion-recovery")
		}
		return result.Artifact, nil
	}
	deadline, _ := ctx.Deadline()
	request := contracts.RequestContext{RequestID: contracts.ID(nonce()), ProfileDigest: started.Job.ProfileDigest, Deadline: deadline}
	var lease contracts.Lease
	if job.State == "running" && workerID.Valid && workerID.String == string(s.m.Worker) && attemptID.Valid && until.Valid {
		expires, err := time.Parse("2006-01-02T15:04:05.000000000Z", until.String)
		if err != nil {
			return empty, stageError("lease")
		}
		if expires.After(time.Now()) {
			lease = contracts.Lease{JobID: job.ID, WorkerID: s.m.Worker, AttemptID: contracts.ID(attemptID.String), FencingToken: job.FencingToken, ExpiresAt: expires}
		}
	}
	if lease.JobID == "" {
		claimed, err := tasks.Claim(ctx, document.PublicationClaimRequest{CommandID: contracts.ID(nonce()), ClaimRequest: contracts.ClaimRequest{Request: request, Scope: scope, JobID: job.ID, WorkerID: s.m.Worker, LeaseDuration: 30 * time.Second}})
		if err != nil {
			return empty, stageError("claim")
		}
		lease = claimed.Lease
	} else {
		renewed, err := tasks.Renew(ctx, document.PublicationRenewRequest{Request: request, Scope: scope, Lease: lease, Duration: 30 * time.Second, CommandID: contracts.ID(nonce())})
		if err != nil {
			return empty, stageError("renew")
		}
		lease = renewed.Lease
	}
	if err = checkpoint(hook, "claimed"); err != nil {
		return empty, err
	}
	access := document.PublicationReadWorkRequest{Request: request, Scope: scope, Lease: lease}
	backend := postgres.PreparationIndex{Tasks: tasks, ModuleInstanceID: s.profile.Index.Bindings[contracts.IndexWriting].InstanceID}
	w := preparation.Worker{Tasks: tasks, Splitter: chunk.SimpleSplitter{}, SplitterInstanceID: s.profile.Index.Bindings[contracts.Splitting].InstanceID, Backend: backend, ResolveProfile: func(_ context.Context, b document.PublicationBackendSnapshot) (contracts.IndexProfile, error) {
		if b.ProfileDigest != s.profile.Index.Digest || b.IndexProfile.ID != s.profile.Index.ID || b.IndexProfile.Version != s.profile.Index.Version {
			return contracts.IndexProfile{}, errState
		}
		return s.profile.Index, nil
	}}
	artifact, err := w.Prepare(ctx, access)
	if err != nil {
		return empty, stageError("prepare")
	}
	if err = checkpoint(hook, "prepared"); err != nil {
		return empty, err
	}
	work, err := tasks.ReadOwned(ctx, access)
	if err != nil {
		return empty, stageError("owned-work")
	}
	completion, err := (postgres.PublicationCompletion{Tasks: tasks, ModuleInstanceID: backend.ModuleInstanceID}).Complete(ctx, preparation.CompletionRequest{Access: access, CommandID: contracts.ID("complete-" + string(lease.AttemptID)), ArtifactDigest: artifact.Digest, ExpectedJobVersion: work.Job.RowVersion})
	if err != nil {
		return empty, stageError("complete")
	}
	if err = checkpoint(hook, "completed"); err != nil {
		return empty, err
	}
	return completion.Artifact, nil
}
