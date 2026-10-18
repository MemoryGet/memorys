package document_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type retryAuth func(context.Context, contracts.Credential) (contracts.Principal, error)

func (f retryAuth) Authenticate(c context.Context, q contracts.Credential) (contracts.Principal, error) {
	return f(c, q)
}

type retryReader func(context.Context, contracts.JobReadRequest) (contracts.ResourceJob, error)

func (f retryReader) ReadResourceJob(c context.Context, q contracts.JobReadRequest) (contracts.ResourceJob, error) {
	return f(c, q)
}

type retryAuthorizer func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error)

func (f retryAuthorizer) Authorize(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return f(c, q)
}

type retryControl struct {
	called  bool
	request contracts.DeletionCleanupControlRequest
}

func (c *retryControl) CancelDeletionCleanup(context.Context, contracts.DeletionCleanupControlRequest) (contracts.ResourceJob, error) {
	return contracts.ResourceJob{}, errors.New("cancel should not be called")
}
func (c *retryControl) RetryDeletionCleanup(_ context.Context, q contracts.DeletionCleanupControlRequest) (contracts.ResourceJob, error) {
	c.called = true
	c.request = q
	return contracts.ResourceJob{ResourceID: "doc", KnowledgeBaseID: "kb", Job: contracts.Job{ID: "job", SpaceID: "space", State: "running", RowVersion: 3, ProfileDigest: contracts.Digest(strings.Repeat("a", 64))}}, nil
}

func TestJobRetryBindsFailedVersionAndControlScope(t *testing.T) {
	control := &retryControl{}
	service := document.JobRetryService{
		Auth: retryAuth(func(_ context.Context, _ contracts.Credential) (contracts.Principal, error) {
			return contracts.Principal{SubjectID: "operator", Issuer: "issuer"}, nil
		}),
		Reader: retryReader(func(_ context.Context, q contracts.JobReadRequest) (contracts.ResourceJob, error) {
			return contracts.ResourceJob{ResourceID: "doc", KnowledgeBaseID: "kb", Job: contracts.Job{ID: q.JobID, SpaceID: "space", State: "failed", RowVersion: 2, ProfileDigest: contracts.Digest(strings.Repeat("a", 64))}}, nil
		}),
		Authorizer: retryAuthorizer(func(_ context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
			return contracts.AccessScope{ScopeID: "control", SubjectID: q.Principal.SubjectID, Action: "publication.cleanup.control", SpaceID: q.SpaceID, KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}, nil
		}),
		Control: control,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	secret := []byte("secret")
	_, err := service.RetryJob(ctx, document.JobRetryInput{Credential: contracts.Credential{Kind: "access_token", Secret: secret}, Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, JobID: "job", ExpectedVersion: 2, IdempotencyKey: "retry-key"})
	if err != nil || !control.called || control.request.ExpectedJobVersion != 2 || control.request.Scope.Action != "publication.cleanup.control" || control.request.Request.ProfileDigest != contracts.Digest(strings.Repeat("a", 64)) {
		t.Fatalf("retry control binding failed: err=%v called=%v request=%+v", err, control.called, control.request)
	}
	if string(secret) != "secret" {
		t.Fatal("caller credential mutated")
	}
}

func TestJobRetryRejectsNonFailedRecordBeforeControl(t *testing.T) {
	called := false
	service := document.JobRetryService{
		Auth: retryAuth(func(context.Context, contracts.Credential) (contracts.Principal, error) {
			return contracts.Principal{SubjectID: "operator", Issuer: "issuer"}, nil
		}),
		Reader: retryReader(func(context.Context, contracts.JobReadRequest) (contracts.ResourceJob, error) {
			return contracts.ResourceJob{ResourceID: "doc", KnowledgeBaseID: "kb", Job: contracts.Job{ID: "job", SpaceID: "space", State: "running", RowVersion: 2, ProfileDigest: contracts.Digest(strings.Repeat("a", 64))}}, nil
		}),
		Authorizer: retryAuthorizer(func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error) {
			return contracts.AccessScope{}, nil
		}),
		Control: retryControlFunc(func(context.Context, contracts.DeletionCleanupControlRequest) (contracts.ResourceJob, error) {
			called = true
			return contracts.ResourceJob{}, nil
		}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := service.RetryJob(ctx, document.JobRetryInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("secret")}, Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, JobID: "job", ExpectedVersion: 2, IdempotencyKey: "retry-key"})
	if !errors.Is(err, document.ErrJobRetryDenied) || called {
		t.Fatalf("non-failed job reached retry control: err=%v called=%v", err, called)
	}
}

type retryControlFunc func(context.Context, contracts.DeletionCleanupControlRequest) (contracts.ResourceJob, error)

func (f retryControlFunc) CancelDeletionCleanup(context.Context, contracts.DeletionCleanupControlRequest) (contracts.ResourceJob, error) {
	return contracts.ResourceJob{}, errors.New("cancel should not be called")
}
func (f retryControlFunc) RetryDeletionCleanup(c context.Context, q contracts.DeletionCleanupControlRequest) (contracts.ResourceJob, error) {
	return f(c, q)
}
