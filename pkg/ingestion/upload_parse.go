package ingestion

import (
	"context"
	"errors"
	"io"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrUploadParse = errors.New("upload parse task unavailable")

type ParseTaskCreate struct {
	Space, KnowledgeBase contracts.ID
	Job                  contracts.Job
	Command              DraftParseCommand
}

type ParseTaskCreator interface {
	CreateParseTask(context.Context, ParseTaskCreate) error
}

// UploadParseService binds the original BlobRef to one queued parser command.
// A failed task creation compensates the blob through the caller's existing
// scope, so this boundary does not leave an unreferenced original behind.
type UploadParseService struct {
	Upload  UploadService
	Creator ParseTaskCreator
}

func (s UploadParseService) UploadAndEnqueue(ctx context.Context, write contracts.BlobWriteRequest, reader io.Reader, principal contracts.Principal, documentID, draftID, candidateID contracts.ID, expectedVersion uint64, format string, options contracts.ParseOptions, job contracts.Job) (contracts.BlobRef, error) {
	if s.Upload.Store == nil || s.Creator == nil || reader == nil || documentID == "" || draftID == "" || candidateID == "" || expectedVersion == 0 || format == "" || job.ID == "" || len(write.Scope.KnowledgeBaseIDs) != 1 {
		return contracts.BlobRef{}, ErrUploadParse
	}
	blob, err := s.Upload.Upload(ctx, write, reader)
	if err != nil {
		return contracts.BlobRef{}, err
	}
	command := DraftParseCommand{Request: write.Request, Principal: principal, Scope: write.Scope, DocumentID: documentID, DraftID: draftID, CandidateID: candidateID, ExpectedVersion: expectedVersion, Input: blob, Format: format, Options: options}
	if err := s.Creator.CreateParseTask(ctx, ParseTaskCreate{Space: write.Scope.SpaceID, KnowledgeBase: write.Scope.KnowledgeBaseIDs[0], Job: job, Command: command}); err != nil {
		_ = s.Upload.Store.Delete(ctx, contracts.BlobReadRequest{Request: write.Request, Scope: write.Scope, Blob: blob})
		return contracts.BlobRef{}, ErrUploadParse
	}
	return blob, nil
}
