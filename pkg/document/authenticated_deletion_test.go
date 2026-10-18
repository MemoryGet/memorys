package document_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type deleteAuth func(context.Context, contracts.Credential) (contracts.Principal, error)

func (f deleteAuth) Authenticate(c context.Context, q contracts.Credential) (contracts.Principal, error) {
	return f(c, q)
}

type deleteAuthority func(context.Context, contracts.Principal, contracts.ID) (contracts.DeletionTarget, error)

func (f deleteAuthority) AuthorizeDocumentDeletion(c context.Context, p contracts.Principal, id contracts.ID) (contracts.DeletionTarget, error) {
	return f(c, p, id)
}

func TestAuthenticatedDeletionBindsCredentialsResourceAndFinalAuthority(t *testing.T) {
	for _, mode := range []string{"ok", "wrong-action", "foreign-document", "token-revoked", "policy-changed"} {
		t.Run(mode, func(t *testing.T) {
			r := deletionRequest()
			revoked, changed := false, false
			authCalls, stores := 0, 0
			var escaped document.DeletionControl
			var bound contracts.AccessScope
			service := document.AuthenticatedDeletion{
				Auth: deleteAuth(func(_ context.Context, c contracts.Credential) (contracts.Principal, error) {
					authCalls++
					if revoked || string(c.Secret) != "credential" {
						return contracts.Principal{}, errors.New("private auth error")
					}
					c.Secret[0] = 'x' // The provider receives its own copy on every call.
					return r.Command.Principal, nil
				}),
				Authority: deleteAuthority(func(_ context.Context, p contracts.Principal, id contracts.ID) (contracts.DeletionTarget, error) {
					scope := r.Command.Scope
					if changed {
						scope.PolicyVersion++
					}
					if mode == "wrong-action" {
						scope.Action = "knowledge.read"
					}
					if mode == "foreign-document" {
						id = "foreign"
					}
					return contracts.DeletionTarget{DocumentID: id, SpaceID: "space", KnowledgeBaseID: "kb", Scope: scope}, nil
				}),
				Stores: func(control document.DeletionControl) (contracts.DocumentDeletionStore, error) {
					stores++
					escaped = control
					return deletionStore(func(c context.Context, q contracts.DocumentDeletionCommand) (contracts.DeleteDocumentResult, error) {
						bound = q.Request.Command.Scope
						if mode == "policy-changed" {
							changed = true
						}
						if e := control.VerifyScope(c, bound); e != nil {
							return contracts.DeleteDocumentResult{}, e
						}
						if mode == "token-revoked" {
							revoked = true
						}
						return contracts.DeleteDocumentResult{OperationID: q.OperationID, HistoryID: q.HistoryID, JobID: q.JobID, DocumentID: "doc", DocumentVersion: 3, PublicationEpoch: 1, DeletedAt: q.OccurredAt}, nil
					}), nil
				},
			}
			credential := contracts.Credential{Kind: "access_token", Secret: []byte("credential")}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			out, e := service.DeleteDocument(ctx, document.AuthenticatedDeleteInput{Credential: credential, Request: r.Command.Request, DocumentID: "doc", ExpectedDocumentVersion: 2, IdempotencyKey: "key"})
			if string(credential.Secret) != "credential" {
				t.Fatal("caller credential mutated")
			}
			if mode == "ok" {
				if e != nil || out.JobID == "" || authCalls < 4 {
					t.Fatal("real authentication refresh missing", e, authCalls)
				}
				if e = escaped.VerifyScope(ctx, bound); e == nil {
					t.Fatal("escaped control remained usable")
				}
			} else if e == nil || out.DocumentID != "" {
				t.Fatal("invalid identity/authority returned deletion result")
			}
			if (mode == "wrong-action" || mode == "foreign-document") && stores != 0 {
				t.Fatal("invalid target reached store factory")
			}
		})
	}
}
