package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrPublicationRetry = errors.New("publication retry unavailable")

// PublicationRetry starts a new publication intent from the old intent's
// server-owned revision/approval binding. It never resets the failed task.
type PublicationRetry struct {
	DB            *sql.DB
	Authorizer    contracts.Authorizer
	Verify        VerifyScope
	ProfileDigest contracts.Digest
}

var _ contracts.PublicationTaskRetrier = PublicationRetry{}

func (p PublicationRetry) RetryPublicationTask(ctx context.Context, input contracts.PublicationTaskRetryRequest) (contracts.PublicationTaskRetryResult, error) {
	var empty contracts.PublicationTaskRetryResult
	if ctx == nil || p.DB == nil || p.Authorizer == nil || input.Scope.SpaceID == "" || len(input.Scope.KnowledgeBaseIDs) != 1 || input.Principal.SubjectID == "" || input.JobID == "" || input.ExpectedJobVersion == 0 || input.IdempotencyKey == "" {
		return empty, ErrPublicationRetry
	}
	var raw []byte
	var state, version, documentID, revisionID, approvalID, epochText string
	if err := p.DB.QueryRowContext(ctx, `SELECT h.payload,h.state,h.row_version::text,i.document_id,i.revision_id,i.approval_id,i.expected_epoch::text FROM akos_withdraw_v1.publication_task_heads h JOIN akos_withdraw_v1.publication_intents i ON i.space_id=h.space_id AND i.kb_id=h.kb_id AND i.job_id=h.id WHERE h.space_id=$1::text AND h.kb_id=$2::text AND h.id=$3::text`, string(input.Scope.SpaceID), string(input.Scope.KnowledgeBaseIDs[0]), string(input.JobID)).Scan(&raw, &state, &version, &documentID, &revisionID, &approvalID, &epochText); err != nil || len(raw) == 0 || len(raw) > 32768 {
		return empty, ErrPublicationRetry
	}
	var old contracts.Job
	if json.Unmarshal(raw, &old) != nil || old.ID != input.JobID || old.State != "failed" && old.State != "paused" || old.RowVersion != input.ExpectedJobVersion || strconv.FormatUint(old.RowVersion, 10) != version || old.SpaceID != input.Scope.SpaceID {
		return empty, ErrPublicationRetry
	}
	epoch, err := strconv.ParseUint(epochText, 10, 64)
	if err != nil {
		return empty, ErrPublicationRetry
	}
	startScope, err := p.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: input.Principal, Action: "publication.start", SpaceID: input.Scope.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), input.Scope.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
	if err != nil || startScope.Action != "publication.start" || startScope.SubjectID != input.Principal.SubjectID || startScope.SpaceID != input.Scope.SpaceID || len(startScope.KnowledgeBaseIDs) != 1 || startScope.KnowledgeBaseIDs[0] != input.Scope.KnowledgeBaseIDs[0] || !startScope.ExpiresAt.After(time.Now()) {
		return empty, ErrPublicationRetry
	}
	profile := input.Request.ProfileDigest
	if profile == "" {
		profile = p.ProfileDigest
	}
	if len(profile) != 64 {
		return empty, ErrPublicationRetry
	}
	verify := func(cx context.Context, before contracts.AccessScope) error {
		if p.Verify != nil && p.Verify(cx, before) != nil {
			return ErrRepository
		}
		fresh, authErr := p.Authorizer.Authorize(cx, contracts.AuthorizeRequest{Principal: input.Principal, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...), ResourceIDs: []contracts.ID{contracts.ID(documentID)}})
		if authErr != nil || fresh.Action != before.Action || fresh.SubjectID != before.SubjectID || fresh.SpaceID != before.SpaceID || !reflect.DeepEqual(fresh.KnowledgeBaseIDs, before.KnowledgeBaseIDs) || fresh.PolicyVersion != before.PolicyVersion || !fresh.ExpiresAt.After(time.Now()) {
			return ErrRepository
		}
		return nil
	}
	start := PublicationStarts{DB: p.DB, Verify: verify, ProfileDigest: profile, AuthorizeStart: func(cx context.Context, request contracts.PublishRequest) error {
		if request.DocumentID != contracts.ID(documentID) || request.RevisionID != contracts.ID(revisionID) || request.ApprovalID != contracts.ID(approvalID) || request.ExpectedEpoch != epoch || request.Command.Principal.SubjectID != input.Principal.SubjectID {
			return ErrRepository
		}
		return nil
	}}
	command := contracts.CommandContext{Request: input.Request, Principal: input.Principal, Scope: startScope, IdempotencyKey: input.IdempotencyKey}
	command.Request.ProfileDigest = profile
	result, err := start.StartPublication(ctx, contracts.PublishRequest{Command: command, DocumentID: contracts.ID(documentID), RevisionID: contracts.ID(revisionID), ApprovalID: contracts.ID(approvalID), ExpectedEpoch: epoch})
	if err != nil || result.Job.ID == "" || result.Job.ID == input.JobID || result.Job.State != "queued" || result.Job.RowVersion != 1 {
		return empty, ErrPublicationRetry
	}
	return contracts.PublicationTaskRetryResult{PreviousJobID: input.JobID, Job: contracts.ResourceJob{ResourceID: contracts.ID(documentID), KnowledgeBaseID: input.Scope.KnowledgeBaseIDs[0], Job: result.Job}}, nil
}
