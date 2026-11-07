package document

import (
	"context"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// PublicationJobCancellationService controls preparation jobs without
// changing publication intent, approval, revision or backend generation.
type PublicationJobCancellationService struct {
	Auth       contracts.Authenticator
	Reader     contracts.ResourceJobReader
	Authorizer contracts.Authorizer
	Control    contracts.PublicationTaskControl
}

// PublicationJobPauseService pauses preparation without changing the
// publication intent, approval, revision or backend generation.
type PublicationJobPauseService struct {
	Auth       contracts.Authenticator
	Reader     contracts.ResourceJobReader
	Authorizer contracts.Authorizer
	Control    contracts.PublicationTaskControl
}

func (p PublicationJobPauseService) PauseJob(ctx context.Context, in JobCancellationInput) (contracts.ResourceJob, error) {
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
	if err != nil || record.Job.ID != in.JobID || record.Job.RowVersion != in.ExpectedVersion || record.Job.State != "queued" && record.Job.State != "running" || record.ResourceID == "" || record.KnowledgeBaseID == "" {
		return empty, ErrJobCancellationDenied
	}
	scope, err := p.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "publication.publish", SpaceID: record.Job.SpaceID, KnowledgeBaseIDs: []contracts.ID{record.KnowledgeBaseID}, ResourceIDs: []contracts.ID{record.ResourceID}})
	if err != nil || scope.Action != "publication.publish" || scope.SubjectID != who.SubjectID || scope.SpaceID != record.Job.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != record.KnowledgeBaseID || !scope.ExpiresAt.After(time.Now()) {
		return empty, ErrJobCancellationDenied
	}
	request := in.Request
	request.ProfileDigest = record.Job.ProfileDigest
	result, err := p.Control.PausePublicationTask(ctx, contracts.PublicationTaskControlRequest{Request: request, Scope: scope, JobID: in.JobID, ExpectedJobVersion: in.ExpectedVersion})
	if err != nil || result.Job.ID != in.JobID || result.Job.State != "paused" || result.Job.RowVersion != in.ExpectedVersion+1 || result.ResourceID != record.ResourceID || result.KnowledgeBaseID != record.KnowledgeBaseID {
		return empty, ErrJobCancellation
	}
	return result, nil
}

func (p PublicationJobCancellationService) CancelJob(ctx context.Context, in JobCancellationInput) (contracts.ResourceJob, error) {
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
	if err != nil || record.Job.ID != in.JobID || record.Job.RowVersion != in.ExpectedVersion || (record.Job.State != "queued" && record.Job.State != "running") || record.ResourceID == "" || record.KnowledgeBaseID == "" {
		return empty, ErrJobCancellationDenied
	}
	scope, err := p.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "publication.publish", SpaceID: record.Job.SpaceID, KnowledgeBaseIDs: []contracts.ID{record.KnowledgeBaseID}, ResourceIDs: []contracts.ID{record.ResourceID}})
	if err != nil || scope.Action != "publication.publish" || scope.SubjectID != who.SubjectID || scope.SpaceID != record.Job.SpaceID || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != record.KnowledgeBaseID || !scope.ExpiresAt.After(time.Now()) {
		return empty, ErrJobCancellationDenied
	}
	request := in.Request
	request.ProfileDigest = record.Job.ProfileDigest
	result, err := p.Control.CancelPublicationTask(ctx, contracts.PublicationTaskControlRequest{Request: request, Scope: scope, JobID: in.JobID, ExpectedJobVersion: in.ExpectedVersion})
	if err != nil || result.Job.ID != in.JobID || result.Job.RowVersion != in.ExpectedVersion+1 || result.ResourceID != record.ResourceID || result.KnowledgeBaseID != record.KnowledgeBaseID {
		return empty, ErrJobCancellation
	}
	return result, nil
}
