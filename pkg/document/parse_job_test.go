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

type parseJobAuth func(context.Context, contracts.Credential) (contracts.Principal, error)

func (f parseJobAuth) Authenticate(ctx context.Context, c contracts.Credential) (contracts.Principal, error) {
	return f(ctx, c)
}

type parseJobReader func(context.Context, contracts.JobReadRequest) (contracts.ResourceJob, error)

func (f parseJobReader) ReadResourceJob(ctx context.Context, q contracts.JobReadRequest) (contracts.ResourceJob, error) {
	return f(ctx, q)
}

type parseJobAuthorizer func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error)

func (f parseJobAuthorizer) Authorize(ctx context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return f(ctx, q)
}

type parseJobControl struct {
	request contracts.ParseTaskControlRequest
}

func (c *parseJobControl) CancelParseTask(_ context.Context, q contracts.ParseTaskControlRequest) (contracts.ResourceJob, error) {
	c.request = q
	return contracts.ResourceJob{ResourceID: "doc", KnowledgeBaseID: "kb", Job: contracts.Job{ID: "job", SpaceID: "space", State: "cancelled", RowVersion: 3}}, nil
}
func (c *parseJobControl) RetryParseTask(_ context.Context, q contracts.ParseTaskControlRequest) (contracts.ResourceJob, error) {
	c.request = q
	return contracts.ResourceJob{ResourceID: "doc", KnowledgeBaseID: "kb", Job: contracts.Job{ID: "job", SpaceID: "space", State: "queued", RowVersion: 3}}, nil
}

func (c *parseJobControl) PauseParseTask(_ context.Context, q contracts.ParseTaskControlRequest) (contracts.ResourceJob, error) {
	c.request = q
	return contracts.ResourceJob{Job: contracts.Job{ID: q.JobID, State: "paused", RowVersion: q.ExpectedJobVersion + 1}}, nil
}

func TestParseJobRetryUsesDocumentCreateScopeAndVersion(t *testing.T) {
	control := &parseJobControl{}
	profile := contracts.Digest(strings.Repeat("a", 64))
	service := document.ParseJobRetryService{
		Auth: parseJobAuth(func(context.Context, contracts.Credential) (contracts.Principal, error) {
			return contracts.Principal{SubjectID: "operator", Issuer: "issuer"}, nil
		}),
		Reader: parseJobReader(func(_ context.Context, q contracts.JobReadRequest) (contracts.ResourceJob, error) {
			return contracts.ResourceJob{ResourceID: "doc", KnowledgeBaseID: "kb", Job: contracts.Job{ID: q.JobID, SpaceID: "space", State: "failed", RowVersion: 2, ProfileDigest: profile}}, nil
		}),
		Authorizer: parseJobAuthorizer(func(_ context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
			if q.Action != "document.create" || q.ResourceIDs[0] != "doc" {
				return contracts.AccessScope{}, errors.New("wrong authorization")
			}
			return contracts.AccessScope{ScopeID: "scope", SubjectID: q.Principal.SubjectID, Action: "document.create", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}, nil
		}),
		Control: control,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := service.RetryJob(ctx, document.JobRetryInput{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("token")}, Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, JobID: "job", ExpectedVersion: 2, IdempotencyKey: "retry"})
	if err != nil || control.request.ExpectedJobVersion != 2 || control.request.Scope.Action != "document.create" || control.request.Request.ProfileDigest != profile {
		t.Fatalf("retry err=%v request=%+v", err, control.request)
	}
}
