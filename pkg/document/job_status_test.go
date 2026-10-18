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

type jobReader func(context.Context, contracts.JobReadRequest) (contracts.ResourceJob, error)

func (f jobReader) ReadResourceJob(c context.Context, q contracts.JobReadRequest) (contracts.ResourceJob, error) {
	return f(c, q)
}

func TestJobStatusReauthenticatesAndKeepsResourceBinding(t *testing.T) {
	for _, mode := range []string{"ok", "denied", "revoked-during-read", "owner-changed", "wrong-id", "version-regressed", "mutate-input"} {
		t.Run(mode, func(t *testing.T) {
			principal := contracts.Principal{SubjectID: "viewer", SubjectType: "user", Issuer: "fixture", ClientID: "client"}
			revoked := false
			authCalls, reads := 0, 0
			service := document.JobStatusService{Auth: deleteAuth(func(_ context.Context, c contracts.Credential) (contracts.Principal, error) {
				authCalls++
				if revoked || string(c.Secret) != "secret" {
					return contracts.Principal{}, errors.New("private auth error")
				}
				c.Secret[0] = 'x'
				return principal, nil
			}), Reader: jobReader(func(_ context.Context, q contracts.JobReadRequest) (contracts.ResourceJob, error) {
				reads++
				if q.Principal != principal || q.JobID != "job" {
					t.Fatal("authenticated identity or target changed")
				}
				if mode == "denied" {
					return contracts.ResourceJob{}, contracts.ErrJobReadDenied
				}
				record := contracts.ResourceJob{ResourceID: "doc", Job: contracts.Job{ID: "job", SpaceID: "space", OperationID: "operation", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Stage: "document.delete.cleanup", State: "queued", RowVersion: 2}}
				if mode == "revoked-during-read" {
					revoked = true
				}
				if mode == "owner-changed" && reads > 1 {
					record.ResourceID = "other-doc"
				}
				if mode == "wrong-id" {
					record.Job.ID = "other-job"
				}
				if mode == "version-regressed" && reads > 1 {
					record.Job.RowVersion = 1
				}
				if mode == "mutate-input" {
					q.Principal.SubjectID = "mutated"
				}
				return record, nil
			})}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			credential := contracts.Credential{Kind: "access_token", Secret: []byte("secret")}
			out, e := service.GetJob(ctx, document.JobStatusInput{Credential: credential, JobID: "job", Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}})
			if mode == "ok" || mode == "mutate-input" {
				if e != nil || out.ResourceID != "doc" || authCalls != 3 || reads != 2 {
					t.Fatal("job status checks missing", e, authCalls, reads)
				}
			} else if e == nil || out.Job.ID != "" {
				t.Fatal("untrusted job status escaped")
			}
			if string(credential.Secret) != "secret" {
				t.Fatal("caller credential mutated")
			}
		})
	}
}
