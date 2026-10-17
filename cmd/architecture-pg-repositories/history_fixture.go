package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

var historicalTarget struct {
	Document, OldRevision, NewRevision, OldApproval, OldPublication contracts.ID
	OldPublicationEpoch                                             uint64
	OldHistory, NewHistory                                          contracts.PublicationHistory
	OldGeneration, UnpublishedRevision                              contracts.ID
	UnpublishedApproval                                             contracts.Approval
}

// Seed a human-edit fixture, then use the real submit/review/prepare/complete/
// publish services to replace an original-backed publication with a new revision.
func createHistoricalReplacement(ctx context.Context, db *sql.DB, original publicationTaskFixture, old preparation.PublicationResult, command contracts.CommandContext, tasks postgres.PublicationTasks, worker preparation.Worker, backend postgres.PreparationIndex, finalizer postgres.PublicationFinalizer) error {
	doc, oldRevision := original.Approved.Revision.DocumentID, original.Approved.Revision.ID
	draft := contracts.Draft{ID: "historical-replacement-draft", DocumentID: doc, BaseRevisionID: &oldRevision, State: "draft", RowVersion: 1, Blocks: []contracts.DraftBlock{{ID: "historical-current-block", DraftID: "historical-replacement-draft", RowVersion: 1, Content: contracts.Content{Kind: contracts.TextBlock, Text: "当前新版", Source: contracts.SourceAnchor{Kind: "revision", RevisionBlockID: "historical-current-block", Locator: contracts.Locator{Precision: "coarse"}}}}}}
	raw, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads VALUES('space','kb',$1,$2,1,'draft',$3::jsonb)`, doc, draft.ID, string(raw)); err != nil {
		return err
	}
	command.Principal = contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "fixture"}
	command.Scope.SubjectID = "actor"
	command.Scope.Action = "document.submit"
	command.Scope.ScopeID = "historical-submit"
	command.IdempotencyKey = "historical-submit"
	verify := func(c context.Context, s contracts.AccessScope) error {
		if c.Err() != nil || s.SpaceID != "space" || len(s.KnowledgeBaseIDs) != 1 || s.KnowledgeBaseIDs[0] != "kb" {
			return errors.New("historical fixture scope denied")
		}
		return nil
	}
	submitter := postgres.Submissions{DB: db, Verify: verify, ProfileDigest: command.Request.ProfileDigest, ReviewPolicyVersion: original.Approved.Approval.PolicyVersion, AuthorizeSubmission: func(c context.Context, q document.SubmissionCommand) error {
		if verify(c, q.Request.Command.Scope) != nil || q.DocumentID != doc || q.Request.Command.Scope.Action != "document.submit" || q.Request.Command.Principal.SubjectID != "actor" {
			return errors.New("historical submit fixture denied")
		}
		return nil
	}, VerifyContent: func(c context.Context, _ contracts.AccessScope, d contracts.Draft) error {
		if c.Err() != nil || d.ID != draft.ID || len(d.Blobs) != 0 || len(d.Blocks) != 1 {
			return errors.New("historical edited draft differs")
		}
		return nil
	}}
	submitted, err := submitter.SubmitDraft(ctx, document.SubmissionCommand{DocumentID: doc, Request: contracts.SubmitRequest{Command: command, DraftID: draft.ID, ExpectedVersion: 1}})
	if err != nil {
		return err
	}
	command.Principal.SubjectID = "reviewer"
	command.Scope.SubjectID = "reviewer"
	command.Scope.Action = "revision.approve"
	command.IdempotencyKey = "historical-approve"
	reviews := postgres.Reviews{DB: db, Verify: verify, ProfileDigest: command.Request.ProfileDigest, ReviewPolicyVersion: original.Approved.Approval.PolicyVersion, AuthorizeDecision: func(c context.Context, q document.ReviewCommand) error {
		if c.Err() != nil || q.DocumentID != doc || q.Request.Command.Principal.SubjectID != "reviewer" {
			return errors.New("historical reviewer fixture denied")
		}
		return nil
	}}
	approval, err := reviews.DecideReview(ctx, document.ReviewCommand{DocumentID: doc, Request: contracts.ApprovalDecisionRequest{Command: command, RevisionID: submitted.Revision.ID, Decision: "approve", Reason: "历史换版原生验收"}})
	if err != nil {
		return err
	}
	command.Principal.SubjectID = "publisher"
	command.Scope.SubjectID = "publisher"
	command.Scope.Action = "publication.start"
	command.IdempotencyKey = "historical-start"
	starts := postgres.PublicationStarts{DB: db, Verify: verify, ProfileDigest: command.Request.ProfileDigest, AuthorizeStart: func(c context.Context, q contracts.PublishRequest) error {
		if c.Err() != nil || q.DocumentID != doc || q.Command.Principal.SubjectID != "publisher" {
			return errors.New("historical publisher fixture denied")
		}
		return nil
	}}
	started, err := starts.StartPublication(ctx, contracts.PublishRequest{Command: command, DocumentID: doc, RevisionID: submitted.Revision.ID, ApprovalID: approval.ID, ExpectedEpoch: 1})
	if err != nil {
		return err
	}
	localTasks := tasks
	localTasks.AuthorizeTask = func(c context.Context, q document.PublicationTaskAccess) error {
		if q.JobID != started.Job.ID {
			return errors.New("historical task fixture denied")
		}
		return tasks.Verify(c, q.Scope)
	}
	scope := command.Scope
	scope.SubjectID = "worker-a"
	scope.Action = "publication.prepare"
	r := command.Request
	r.ProfileDigest = started.Job.ProfileDigest
	r.RequestID = "historical-prepare"
	lease, err := localTasks.Claim(ctx, document.PublicationClaimRequest{CommandID: "historical-claim", ClaimRequest: contracts.ClaimRequest{Request: r, Scope: scope, JobID: started.Job.ID, WorkerID: "worker-a", LeaseDuration: 4 * time.Second}})
	if err != nil {
		return err
	}
	access := document.PublicationReadWorkRequest{Request: r, Scope: scope, Lease: lease.Lease}
	localBackend := backend
	localBackend.Tasks = localTasks
	localWorker := worker
	localWorker.Tasks = localTasks
	localWorker.Backend = localBackend
	artifact, err := localWorker.Prepare(ctx, access)
	if err != nil {
		return err
	}
	work, err := localTasks.ReadOwned(ctx, access)
	if err != nil {
		return err
	}
	completion, err := (postgres.PublicationCompletion{Tasks: localTasks, ModuleInstanceID: backend.ModuleInstanceID}).Complete(ctx, preparation.CompletionRequest{Access: access, CommandID: "historical-complete", ArtifactDigest: artifact.Digest, ExpectedJobVersion: work.Job.RowVersion})
	if err != nil {
		return err
	}
	command.Scope.Action = "publication.publish"
	command.IdempotencyKey = "historical-publish"
	result, err := finalizer.Finalize(ctx, preparation.PublicationRequest{Command: command, OperationID: started.Operation.ID, ExpectedEpoch: 1, ArtifactDigest: completion.Artifact.Digest})
	if err != nil {
		return err
	}
	if result.Operation.RevisionID != submitted.Revision.ID {
		return errors.New("historical publication revision differs")
	}
	historicalTarget.Document, historicalTarget.OldRevision, historicalTarget.NewRevision = doc, oldRevision, submitted.Revision.ID
	historicalTarget.OldApproval, historicalTarget.OldPublication, historicalTarget.OldPublicationEpoch = original.Approved.Approval.ID, old.History.ID, old.History.PublicationEpoch
	historicalTarget.OldHistory, historicalTarget.NewHistory = old.History, result.History
	parent := submitted.Revision.ID
	draft.ID = "historical-unpublished-draft"
	draft.BaseRevisionID = &parent
	draft.Blocks[0].ID = "historical-unpublished-block"
	draft.Blocks[0].DraftID = draft.ID
	draft.Blocks[0].Content.Text = "未发布草稿"
	draft.Blocks[0].Content.Source.RevisionBlockID = draft.Blocks[0].ID
	raw, err = json.Marshal(draft)
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.draft_heads VALUES('space','kb',$1,$2,1,'draft',$3::jsonb)`, doc, draft.ID, string(raw)); err != nil {
		return err
	}
	command.Principal.SubjectID = "actor"
	command.Scope.SubjectID = "actor"
	command.Scope.Action = "document.submit"
	command.IdempotencyKey = "historical-unpublished-submit"
	unpublished, err := submitter.SubmitDraft(ctx, document.SubmissionCommand{DocumentID: doc, Request: contracts.SubmitRequest{Command: command, DraftID: draft.ID, ExpectedVersion: 1}})
	if err != nil {
		return err
	}
	command.Principal.SubjectID = "reviewer"
	command.Scope.SubjectID = "reviewer"
	command.Scope.Action = "revision.approve"
	command.IdempotencyKey = "historical-unpublished-approve"
	unpublishedApproval, err := reviews.DecideReview(ctx, document.ReviewCommand{DocumentID: doc, Request: contracts.ApprovalDecisionRequest{Command: command, RevisionID: unpublished.Revision.ID, Decision: "approve", Reason: "批准不等于发布"}})
	if err != nil {
		return err
	}
	historicalTarget.UnpublishedRevision, historicalTarget.UnpublishedApproval = unpublished.Revision.ID, unpublishedApproval
	return nil
}
