package document

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrJobCancellation = errors.New("job cancellation unavailable")
var ErrJobCancellationUnauthenticated = errors.New("job cancellation authentication required")
var ErrJobCancellationDenied = errors.New("job cancellation denied")

type JobCancellationInput struct {
	Credential      contracts.Credential
	Request         contracts.RequestContext
	JobID           contracts.ID
	ExpectedVersion uint64
	IdempotencyKey  string
}

type JobCancellationService struct {
	Auth       contracts.Authenticator
	Reader     contracts.ResourceJobReader
	Authorizer contracts.Authorizer
	Control    contracts.DeletionCleanupControl
}

func (p JobCancellationService) CancelJob(ctx context.Context, in JobCancellationInput) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || p.Auth == nil || p.Reader == nil || p.Authorizer == nil || p.Control == nil || !frozenID(in.JobID) || !frozenID(in.Request.RequestID) || in.Request.Deadline.IsZero() || in.ExpectedVersion == 0 || in.IdempotencyKey == "" {
		return empty, ErrJobCancellation
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrJobCancellation
	}
	ctx, cancel := context.WithDeadline(ctx, in.Request.Deadline)
	defer cancel()
	credential := in.Credential
	credential.Secret = append([]byte(nil), credential.Secret...)
	defer clear(credential.Secret)
	who, e := p.Auth.Authenticate(ctx, credential)
	if e != nil || ctx.Err() != nil || !frozenID(who.SubjectID) || who.Issuer == "" {
		return empty, ErrJobCancellationUnauthenticated
	}
	q := contracts.JobReadRequest{Request: in.Request, Principal: who, JobID: in.JobID}
	record, e := p.Reader.ReadResourceJob(ctx, q)
	if e != nil || record.Job.ID != in.JobID || record.Job.RowVersion != in.ExpectedVersion || !frozenID(record.ResourceID) || !frozenID(record.KnowledgeBaseID) || !frozenID(record.Job.SpaceID) || !candidateDigest(record.Job.ProfileDigest) {
		return empty, ErrJobCancellationDenied
	}
	scope, e := p.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "publication.cleanup.control", SpaceID: record.Job.SpaceID, KnowledgeBaseIDs: []contracts.ID{record.KnowledgeBaseID}, ResourceIDs: []contracts.ID{record.ResourceID}})
	if e != nil || scope.Action != "publication.cleanup.control" || scope.SubjectID != who.SubjectID || scope.SpaceID != record.Job.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.ExpiresAt.Before(time.Now()) {
		return empty, ErrJobCancellationDenied
	}
	request := in.Request
	request.ProfileDigest = record.Job.ProfileDigest
	req := contracts.DeletionCleanupControlRequest{Request: request, Scope: scope, JobID: in.JobID, ExpectedJobVersion: in.ExpectedVersion}
	result, e := p.Control.CancelDeletionCleanup(ctx, req)
	if e != nil {
		return empty, ErrJobCancellationDenied
	}
	if result.Job.ID != in.JobID || result.Job.RowVersion < in.ExpectedVersion || !reflect.DeepEqual(result.ResourceID, record.ResourceID) || result.KnowledgeBaseID != record.KnowledgeBaseID {
		return empty, ErrJobCancellation
	}
	return result, nil
}
