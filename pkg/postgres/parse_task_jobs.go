package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/ingestion"
)

var ErrParseTaskJob = errors.New("parse task job unavailable")

// ParseTaskResourceReader binds a parser task to the document in its durable
// DraftParseCommand, then asks the current Authorizer for document.create.
// The task row itself never supplies authorization.
type ParseTaskResourceReader struct {
	DB         *sql.DB
	Authorizer contracts.Authorizer
}

func (r ParseTaskResourceReader) ReadResourceJob(ctx context.Context, input contracts.JobReadRequest) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || r.DB == nil || r.Authorizer == nil || input.JobID == "" || input.Principal.SubjectID == "" {
		return empty, contracts.ErrJobReadDenied
	}
	record, space, kb, err := (ParseTaskRepository{DB: r.DB}).ReadAny(ctx, input.JobID)
	if err != nil {
		return empty, contracts.ErrJobReadDenied
	}
	command, err := ingestion.DecodeParseTaskPayload(record.Command)
	if err != nil || command.Scope.SpaceID != space || len(command.Scope.KnowledgeBaseIDs) != 1 || command.Scope.KnowledgeBaseIDs[0] != kb || command.DocumentID == "" {
		return empty, contracts.ErrJobReadDenied
	}
	scope, err := r.Authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: input.Principal, Action: "document.create", SpaceID: space, KnowledgeBaseIDs: []contracts.ID{kb}, ResourceIDs: []contracts.ID{command.DocumentID}})
	if err != nil || scope.Action != "document.create" || scope.SpaceID != space || len(scope.KnowledgeBaseIDs) != 1 || scope.KnowledgeBaseIDs[0] != kb || !scope.ExpiresAt.After(time.Now()) {
		return empty, contracts.ErrJobReadDenied
	}
	return contracts.ResourceJob{ResourceID: command.DocumentID, KnowledgeBaseID: kb, Job: record.Job}, nil
}

// ParseTaskControl adapts the durable parser repository to the application
// service after that service has reauthorized the current scope.
type ParseTaskControl struct{ DB *sql.DB }

var _ contracts.ParseTaskControl = ParseTaskControl{}

func (c ParseTaskControl) CancelParseTask(ctx context.Context, input contracts.ParseTaskControlRequest) (contracts.ResourceJob, error) {
	if c.DB == nil || input.Scope.SpaceID == "" || len(input.Scope.KnowledgeBaseIDs) != 1 {
		return contracts.ResourceJob{}, ErrParseTaskJob
	}
	record, err := (ParseTaskRepository{DB: c.DB}).Cancel(ctx, input.Scope.SpaceID, input.Scope.KnowledgeBaseIDs[0], input.JobID, input.Scope.SubjectID, input.Request.RequestID, input.ExpectedJobVersion)
	if err != nil {
		return contracts.ResourceJob{}, ErrParseTaskJob
	}
	return parseTaskResourceJob(record, input.Scope.SpaceID, input.Scope.KnowledgeBaseIDs[0])
}

func (c ParseTaskControl) PauseParseTask(ctx context.Context, input contracts.ParseTaskControlRequest) (contracts.ResourceJob, error) {
	if c.DB == nil || input.Scope.SpaceID == "" || len(input.Scope.KnowledgeBaseIDs) != 1 {
		return contracts.ResourceJob{}, ErrParseTaskJob
	}
	record, err := (ParseTaskRepository{DB: c.DB}).Pause(ctx, input.Scope.SpaceID, input.Scope.KnowledgeBaseIDs[0], input.JobID, input.Scope.SubjectID, input.Request.RequestID, input.ExpectedJobVersion)
	if err != nil {
		return contracts.ResourceJob{}, ErrParseTaskJob
	}
	return parseTaskResourceJob(record, input.Scope.SpaceID, input.Scope.KnowledgeBaseIDs[0])
}

func (c ParseTaskControl) RetryParseTask(ctx context.Context, input contracts.ParseTaskControlRequest) (contracts.ResourceJob, error) {
	if c.DB == nil || input.Scope.SpaceID == "" || len(input.Scope.KnowledgeBaseIDs) != 1 {
		return contracts.ResourceJob{}, ErrParseTaskJob
	}
	record, err := (ParseTaskRepository{DB: c.DB}).Retry(ctx, input.Scope.SpaceID, input.Scope.KnowledgeBaseIDs[0], input.JobID, input.Scope.SubjectID, input.Request.RequestID, input.ExpectedJobVersion)
	if err != nil {
		return contracts.ResourceJob{}, ErrParseTaskJob
	}
	return parseTaskResourceJob(record, input.Scope.SpaceID, input.Scope.KnowledgeBaseIDs[0])
}

func parseTaskResourceJob(record ParseTaskRecord, space, kb contracts.ID) (contracts.ResourceJob, error) {
	command, err := ingestion.DecodeParseTaskPayload(record.Command)
	if err != nil || command.Scope.SpaceID != space || len(command.Scope.KnowledgeBaseIDs) != 1 || command.Scope.KnowledgeBaseIDs[0] != kb || command.DocumentID == "" {
		return contracts.ResourceJob{}, ErrParseTaskJob
	}
	return contracts.ResourceJob{ResourceID: command.DocumentID, KnowledgeBaseID: kb, Job: record.Job}, nil
}
