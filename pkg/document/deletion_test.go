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

type deletionStore func(context.Context, contracts.DocumentDeletionCommand) (contracts.DeleteDocumentResult, error)

func (f deletionStore) DeleteDocument(c context.Context, q contracts.DocumentDeletionCommand) (contracts.DeleteDocumentResult, error) {
	return f(c, q)
}
func deletionRequest() contracts.DeleteDocumentRequest {
	return contracts.DeleteDocumentRequest{DocumentID: "doc", ExpectedDocumentVersion: 2, Command: contracts.CommandContext{IdempotencyKey: "delete", Principal: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "fixture", ClientID: "client"}, Request: contracts.RequestContext{RequestID: "request", ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Deadline: time.Now().Add(time.Minute)}, Scope: contracts.AccessScope{ScopeID: "scope", SubjectID: "actor", Action: "document.delete", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}}}
}
func TestDeletionCommitmentsBindIntentNotEphemeralAuthority(t *testing.T) {
	r := deletionRequest()
	actor, command, e := document.DeletionCommitments(r)
	if e != nil {
		t.Fatal(e)
	}
	r.Command.Request.RequestID = "retry"
	r.Command.Scope.ScopeID = "renewed"
	r.Command.Scope.PolicyVersion++
	r.Command.Request.Deadline = time.Now().Add(2 * time.Minute)
	a, b, e := document.DeletionCommitments(r)
	if e != nil || a != actor || b != command {
		t.Fatal("transient retry fields changed stable intent")
	}
	r.ExpectedDocumentVersion++
	a, b, e = document.DeletionCommitments(r)
	if e != nil || a != actor || b == command {
		t.Fatal("document CAS omitted from intent")
	}
	r.Command.Principal.Issuer = "other"
	a, _, e = document.DeletionCommitments(r)
	if e != nil || a == actor {
		t.Fatal("issuer omitted from actor binding")
	}
	r.Command.Scope.Action = "publication.withdraw"
	if _, _, e = document.DeletionCommitments(r); e == nil {
		t.Fatal("withdraw grant enabled deletion")
	}
}
func TestDeletionCopiesAuthorityAndFailsWithoutFinalAuthority(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		r := deletionRequest()
		calls := 0
		service := document.DeletionService{Verify: func(_ context.Context, q contracts.DeleteDocumentRequest) error {
			calls++
			if q.Command.Scope.KnowledgeBaseIDs[0] != "kb" {
				t.Fatal("provider rewrote authority expectation")
			}
			q.Command.Scope.KnowledgeBaseIDs[0] = "provider-mutated"
			if revoke && calls == 2 {
				return errors.New("revoked")
			}
			return nil
		}, Store: deletionStore(func(_ context.Context, q contracts.DocumentDeletionCommand) (contracts.DeleteDocumentResult, error) {
			if q.Request.Command.Scope.KnowledgeBaseIDs[0] != "kb" {
				t.Fatal("verify input mutation reached store")
			}
			q.Request.Command.Scope.KnowledgeBaseIDs[0] = "store-mutated"
			return contracts.DeleteDocumentResult{OperationID: q.OperationID, JobID: q.JobID, HistoryID: q.HistoryID, DocumentID: q.Request.DocumentID, DocumentVersion: 3, PublicationEpoch: 1, DeletedAt: q.OccurredAt}, nil
		})}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		out, e := service.Delete(ctx, r)
		cancel()
		if revoke {
			if e == nil || out.DocumentID != "" {
				t.Fatal("final authority rejection leaked deletion result")
			}
		} else if e != nil || out.DocumentID != "doc" {
			t.Fatal(e)
		}
		if calls != 2 || r.Command.Scope.KnowledgeBaseIDs[0] != "kb" {
			t.Fatal("authority/caller ownership lost")
		}
	}
}
