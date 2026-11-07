package ingestion

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type uploadBlobFake struct{ deleted bool }

func (s *uploadBlobFake) Put(context.Context, contracts.BlobWriteRequest, io.Reader) (contracts.BlobRef, error) {
	return contracts.BlobRef{ID: "blob", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 3, MediaType: "text/plain"}, nil
}
func (s *uploadBlobFake) Open(context.Context, contracts.BlobReadRequest) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (s *uploadBlobFake) Delete(context.Context, contracts.BlobReadRequest) error {
	s.deleted = true
	return nil
}

type uploadTaskFake struct {
	err     error
	created ParseTaskCreate
}

func (s *uploadTaskFake) CreateParseTask(_ context.Context, value ParseTaskCreate) error {
	s.created = value
	return s.err
}

func TestUploadParseServiceBindsBlobToQueuedCommand(t *testing.T) {
	store, creator := &uploadBlobFake{}, &uploadTaskFake{}
	service := UploadParseService{Upload: UploadService{Store: store}, Creator: creator}
	deadline := time.Now().Add(time.Minute)
	blob, err := service.UploadAndEnqueue(context.Background(), contracts.BlobWriteRequest{Request: contracts.RequestContext{RequestID: "request", ProfileDigest: contracts.Digest("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "user", Action: "document.parse", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: deadline}, Size: 3, MediaType: "text/plain"}, bytes.NewReader([]byte("abc")), contracts.Principal{SubjectID: "user", SubjectType: "user", Issuer: "issuer"}, "doc", "draft", "candidate", 1, "txt", contracts.ParseOptions{}, contracts.Job{ID: "job", SpaceID: "space", Stage: "document.parse", State: "queued", RowVersion: 1, Attempt: 1})
	if err != nil || blob.ID != "blob" || creator.created.Command.Input.ID != "blob" || creator.created.Job.ID != "job" {
		t.Fatalf("upload parse binding failed: %+v %v", blob, err)
	}
}

func TestUploadParseServiceCompensatesWhenTaskCreationFails(t *testing.T) {
	store, creator := &uploadBlobFake{}, &uploadTaskFake{err: errors.New("queue down")}
	service := UploadParseService{Upload: UploadService{Store: store}, Creator: creator}
	deadline := time.Now().Add(time.Minute)
	_, err := service.UploadAndEnqueue(context.Background(), contracts.BlobWriteRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: deadline}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "user", Action: "document.parse", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: deadline}, Size: 3, MediaType: "text/plain"}, bytes.NewReader([]byte("abc")), contracts.Principal{SubjectID: "user", SubjectType: "user", Issuer: "issuer"}, "doc", "draft", "candidate", 1, "txt", contracts.ParseOptions{}, contracts.Job{ID: "job", SpaceID: "space", Stage: "document.parse", State: "queued", RowVersion: 1, Attempt: 1})
	if !errors.Is(err, ErrUploadParse) || !store.deleted {
		t.Fatal("orphan blob was not compensated", err)
	}
}
