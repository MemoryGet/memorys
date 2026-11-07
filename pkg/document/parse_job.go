package document

import (
	"context"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrParseJob = errors.New("parse job unavailable")
var ErrParseJobUnauthenticated = errors.New("parse job authentication required")
var ErrParseJobDenied = errors.New("parse job control denied")

// ParseJobCancellationService and ParseJobRetryService share the transport
// input shape with deletion jobs but use document.parse control authorization
// and the parser-specific durable control port.
type ParseJobCancellationService struct {
	Auth       contracts.Authenticator
	Reader     contracts.ResourceJobReader
	Authorizer contracts.Authorizer
	Control    contracts.ParseTaskControl
}

func (p ParseJobCancellationService) CancelParseJob(ctx context.Context, in JobCancellationInput) (contracts.ResourceJob, error) {
	return p.control(ctx, in)
}

func (p ParseJobCancellationService) CancelJob(ctx context.Context, in JobCancellationInput) (contracts.ResourceJob, error) {
	return p.CancelParseJob(ctx, in)
}

type ParseJobRetryService struct {
	Auth       contracts.Authenticator
	Reader     contracts.ResourceJobReader
	Authorizer contracts.Authorizer
	Control    contracts.ParseTaskControl
}

type ParseJobPauseService struct {
	Auth       contracts.Authenticator
	Reader     contracts.ResourceJobReader
	Authorizer contracts.Authorizer
	Control    contracts.ParseTaskControl
}

func (p ParseJobPauseService) PauseJob(ctx context.Context, in JobCancellationInput) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || p.Auth == nil || p.Reader == nil || p.Authorizer == nil || p.Control == nil || !frozenID(in.JobID) || !frozenID(in.Request.RequestID) || in.Request.Deadline.IsZero() || in.ExpectedVersion == 0 || in.IdempotencyKey == "" {
		return empty, ErrJobPause
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrJobPause
	}
	ctx, cancel := context.WithDeadline(ctx, in.Request.Deadline)
	defer cancel()
	who, err := parseJobAuthenticate(ctx, p.Auth, in.Credential)
	if err != nil {
		return empty, ErrJobPauseUnauthenticated
	}
	record, err := p.Reader.ReadResourceJob(ctx, contracts.JobReadRequest{Request: in.Request, Principal: who, JobID: in.JobID})
	if err != nil || record.Job.ID != in.JobID || record.Job.RowVersion != in.ExpectedVersion || record.Job.State != "queued" && record.Job.State != "running" || !frozenID(record.ResourceID) || !frozenID(record.KnowledgeBaseID) {
		return empty, ErrJobPauseDenied
	}
	scope, err := parseJobAuthorize(ctx, p.Authorizer, who, record)
	if err != nil {
		return empty, ErrJobPauseDenied
	}
	request := in.Request
	request.ProfileDigest = record.Job.ProfileDigest
	result, err := p.Control.PauseParseTask(ctx, contracts.ParseTaskControlRequest{Request: request, Scope: scope, JobID: in.JobID, ExpectedJobVersion: in.ExpectedVersion})
	if err != nil || result.Job.ID != in.JobID || result.Job.State != "paused" || result.Job.RowVersion != in.ExpectedVersion+1 || result.ResourceID != record.ResourceID || result.KnowledgeBaseID != record.KnowledgeBaseID {
		return empty, ErrJobPause
	}
	return result, nil
}

var ErrJobPause = errors.New("parse job pause unavailable")
var ErrJobPauseUnauthenticated = errors.New("parse job pause authentication required")
var ErrJobPauseDenied = errors.New("parse job pause denied")

func (p ParseJobRetryService) RetryParseJob(ctx context.Context, in JobRetryInput) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || p.Auth == nil || p.Reader == nil || p.Authorizer == nil || p.Control == nil || !frozenID(in.JobID) || !frozenID(in.Request.RequestID) || in.Request.Deadline.IsZero() || in.ExpectedVersion == 0 || in.IdempotencyKey == "" {
		return empty, ErrJobRetry
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrJobRetry
	}
	ctx, cancel := context.WithDeadline(ctx, in.Request.Deadline)
	defer cancel()
	who, err := parseJobAuthenticate(ctx, p.Auth, in.Credential)
	if err != nil {
		return empty, ErrJobRetryUnauthenticated
	}
	record, err := p.Reader.ReadResourceJob(ctx, contracts.JobReadRequest{Request: in.Request, Principal: who, JobID: in.JobID})
	if err != nil || record.Job.ID != in.JobID || record.Job.RowVersion != in.ExpectedVersion || (record.Job.State != "failed" && record.Job.State != "cancelled" && record.Job.State != "paused") || !frozenID(record.ResourceID) || !frozenID(record.KnowledgeBaseID) {
		return empty, ErrJobRetryDenied
	}
	scope, err := parseJobAuthorize(ctx, p.Authorizer, who, record)
	if err != nil {
		return empty, ErrJobRetryDenied
	}
	request := in.Request
	request.ProfileDigest = record.Job.ProfileDigest
	result, err := p.Control.RetryParseTask(ctx, contracts.ParseTaskControlRequest{Request: request, Scope: scope, JobID: in.JobID, ExpectedJobVersion: in.ExpectedVersion})
	if err != nil || result.Job.ID != in.JobID || result.Job.State != "queued" || result.Job.RowVersion != in.ExpectedVersion+1 || result.ResourceID != record.ResourceID || result.KnowledgeBaseID != record.KnowledgeBaseID {
		return empty, ErrJobRetry
	}
	return result, nil
}

func (p ParseJobRetryService) RetryJob(ctx context.Context, in JobRetryInput) (contracts.ResourceJob, error) {
	return p.RetryParseJob(ctx, in)
}

func (p ParseJobCancellationService) control(ctx context.Context, in JobCancellationInput) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || p.Auth == nil || p.Reader == nil || p.Authorizer == nil || p.Control == nil || !frozenID(in.JobID) || !frozenID(in.Request.RequestID) || in.Request.Deadline.IsZero() || in.ExpectedVersion == 0 || in.IdempotencyKey == "" {
		return empty, ErrJobCancellation
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrJobCancellation
	}
	ctx, cancel := context.WithDeadline(ctx, in.Request.Deadline)
	defer cancel()
	who, err := parseJobAuthenticate(ctx, p.Auth, in.Credential)
	if err != nil {
		return empty, ErrJobCancellationUnauthenticated
	}
	record, err := p.Reader.ReadResourceJob(ctx, contracts.JobReadRequest{Request: in.Request, Principal: who, JobID: in.JobID})
	if err != nil || record.Job.ID != in.JobID || record.Job.RowVersion != in.ExpectedVersion || (record.Job.State != "queued" && record.Job.State != "running") || !frozenID(record.ResourceID) || !frozenID(record.KnowledgeBaseID) {
		return empty, ErrJobCancellationDenied
	}
	scope, err := parseJobAuthorize(ctx, p.Authorizer, who, record)
	if err != nil {
		return empty, ErrJobCancellationDenied
	}
	request := in.Request
	request.ProfileDigest = record.Job.ProfileDigest
	result, err := p.Control.CancelParseTask(ctx, contracts.ParseTaskControlRequest{Request: request, Scope: scope, JobID: in.JobID, ExpectedJobVersion: in.ExpectedVersion})
	if err != nil || result.Job.ID != in.JobID || result.Job.RowVersion != in.ExpectedVersion+1 || result.ResourceID != record.ResourceID || result.KnowledgeBaseID != record.KnowledgeBaseID {
		return empty, ErrJobCancellation
	}
	return result, nil
}

func parseJobAuthenticate(ctx context.Context, auth contracts.Authenticator, credential contracts.Credential) (contracts.Principal, error) {
	copyCredential := contracts.Credential{Kind: credential.Kind, Secret: append([]byte(nil), credential.Secret...)}
	defer clear(copyCredential.Secret)
	who, err := auth.Authenticate(ctx, copyCredential)
	if err != nil || ctx.Err() != nil || !frozenID(who.SubjectID) || who.Issuer == "" {
		return contracts.Principal{}, ErrParseJobUnauthenticated
	}
	return who, nil
}

func parseJobAuthorize(ctx context.Context, authorizer contracts.Authorizer, who contracts.Principal, record contracts.ResourceJob) (contracts.AccessScope, error) {
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "document.create", SpaceID: record.Job.SpaceID, KnowledgeBaseIDs: []contracts.ID{record.KnowledgeBaseID}, ResourceIDs: []contracts.ID{record.ResourceID}})
	if err != nil || scope.Action != "document.create" || scope.SubjectID != who.SubjectID || scope.SpaceID != record.Job.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != record.KnowledgeBaseID || !scope.ExpiresAt.After(time.Now()) {
		return contracts.AccessScope{}, ErrParseJobDenied
	}
	return scope, nil
}
