package document

import (
	"context"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrPublicationRetry = errors.New("publication retry unavailable")
var ErrPublicationRetryUnauthenticated = errors.New("publication retry authentication required")
var ErrPublicationRetryDenied = errors.New("publication retry denied")

type PublicationJobRetryService struct {
	Auth       contracts.Authenticator
	Reader     contracts.ResourceJobReader
	Authorizer contracts.Authorizer
	Retrier    contracts.PublicationTaskRetrier
}

// RetryJob creates a new publication intent/job from the failed task's
// server-owned revision and approval binding. The old job remains failed.
func (p PublicationJobRetryService) RetryJob(ctx context.Context, in JobRetryInput) (contracts.PublicationTaskRetryResult, error) {
	var empty contracts.PublicationTaskRetryResult
	if ctx == nil || p.Auth == nil || p.Reader == nil || p.Authorizer == nil || p.Retrier == nil || !frozenID(in.JobID) || !frozenID(in.Request.RequestID) || in.Request.Deadline.IsZero() || in.ExpectedVersion == 0 || in.IdempotencyKey == "" {
		return empty, ErrPublicationRetry
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrPublicationRetry
	}
	ctx, cancel := context.WithDeadline(ctx, in.Request.Deadline)
	defer cancel()
	who, err := parseJobAuthenticate(ctx, p.Auth, in.Credential)
	if err != nil {
		return empty, ErrPublicationRetryUnauthenticated
	}
	record, err := p.Reader.ReadResourceJob(ctx, contracts.JobReadRequest{Request: in.Request, Principal: who, JobID: in.JobID})
	if err != nil || record.Job.ID != in.JobID || record.Job.RowVersion != in.ExpectedVersion || record.Job.State != "failed" && record.Job.State != "paused" || record.ResourceID == "" || record.KnowledgeBaseID == "" {
		return empty, ErrPublicationRetryDenied
	}
	scope, err := p.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "publication.publish", SpaceID: record.Job.SpaceID, KnowledgeBaseIDs: []contracts.ID{record.KnowledgeBaseID}, ResourceIDs: []contracts.ID{record.ResourceID}})
	if err != nil || scope.Action != "publication.publish" || scope.SubjectID != who.SubjectID || scope.SpaceID != record.Job.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != record.KnowledgeBaseID || !scope.ExpiresAt.After(time.Now()) {
		return empty, ErrPublicationRetryDenied
	}
	request := in.Request
	request.ProfileDigest = record.Job.ProfileDigest
	result, err := p.Retrier.RetryPublicationTask(ctx, contracts.PublicationTaskRetryRequest{Request: request, Scope: scope, Principal: who, JobID: in.JobID, ExpectedJobVersion: in.ExpectedVersion, IdempotencyKey: in.IdempotencyKey})
	if err != nil || result.PreviousJobID != in.JobID || result.Job.Job.ID == "" || result.Job.Job.ID == in.JobID || result.Job.Job.State != "queued" || result.Job.Job.RowVersion != 1 || result.Job.ResourceID != record.ResourceID || result.Job.KnowledgeBaseID != record.KnowledgeBaseID {
		return empty, ErrPublicationRetry
	}
	return result, nil
}
