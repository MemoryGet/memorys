package document

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrJobRetry = errors.New("job retry unavailable")
var ErrJobRetryUnauthenticated = errors.New("job retry authentication required")
var ErrJobRetryDenied = errors.New("job retry denied")

// JobRetryInput deliberately has the same request shape as cancellation. The
// idempotency key remains a transport commitment; the durable control port
// additionally binds the current job row version before changing state.
type JobRetryInput = JobCancellationInput

type JobRetryService struct {
	Auth       contracts.Authenticator
	Reader     contracts.ResourceJobReader
	Authorizer contracts.Authorizer
	Control    contracts.DeletionCleanupControl
}

func (p JobRetryService) RetryJob(ctx context.Context, in JobRetryInput) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || p.Auth == nil || p.Reader == nil || p.Authorizer == nil || p.Control == nil || !frozenID(in.JobID) || !frozenID(in.Request.RequestID) || in.Request.Deadline.IsZero() || in.ExpectedVersion == 0 || in.IdempotencyKey == "" {
		return empty, ErrJobRetry
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrJobRetry
	}
	ctx, cancel := context.WithDeadline(ctx, in.Request.Deadline)
	defer cancel()
	credential := in.Credential
	credential.Secret = append([]byte(nil), credential.Secret...)
	defer clear(credential.Secret)
	who, e := p.Auth.Authenticate(ctx, credential)
	if e != nil || ctx.Err() != nil || !frozenID(who.SubjectID) || who.Issuer == "" {
		return empty, ErrJobRetryUnauthenticated
	}
	record, e := p.Reader.ReadResourceJob(ctx, contracts.JobReadRequest{Request: in.Request, Principal: who, JobID: in.JobID})
	if e != nil || record.Job.ID != in.JobID || record.Job.RowVersion != in.ExpectedVersion || record.Job.State != "failed" || !frozenID(record.ResourceID) || !frozenID(record.KnowledgeBaseID) || !frozenID(record.Job.SpaceID) || !candidateDigest(record.Job.ProfileDigest) {
		return empty, ErrJobRetryDenied
	}
	scope, e := p.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "publication.cleanup.control", SpaceID: record.Job.SpaceID, KnowledgeBaseIDs: []contracts.ID{record.KnowledgeBaseID}, ResourceIDs: []contracts.ID{record.ResourceID}})
	if e != nil || scope.Action != "publication.cleanup.control" || scope.SubjectID != who.SubjectID || scope.SpaceID != record.Job.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || !scope.ExpiresAt.After(time.Now()) {
		return empty, ErrJobRetryDenied
	}
	request := in.Request
	request.ProfileDigest = record.Job.ProfileDigest
	result, e := p.Control.RetryDeletionCleanup(ctx, contracts.DeletionCleanupControlRequest{Request: request, Scope: scope, JobID: in.JobID, ExpectedJobVersion: in.ExpectedVersion})
	if e != nil {
		return empty, ErrJobRetryDenied
	}
	if result.Job.ID != in.JobID || result.Job.State != "running" || result.Job.RowVersion != in.ExpectedVersion+1 || !reflect.DeepEqual(result.ResourceID, record.ResourceID) || result.KnowledgeBaseID != record.KnowledgeBaseID {
		return empty, ErrJobRetry
	}
	return result, nil
}
