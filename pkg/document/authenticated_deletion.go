package document

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrDeletionUnauthenticated = errors.New("deletion authentication required")
var ErrDeletionDenied = errors.New("deletion target unavailable")

type AuthenticatedDeleteInput struct {
	Credential              contracts.Credential
	Request                 contracts.RequestContext
	DocumentID              contracts.ID
	ExpectedDocumentVersion uint64
	IdempotencyKey          string
}

type DeletionControl interface {
	VerifyScope(context.Context, contracts.AccessScope) error
	AuthorizeDelete(context.Context, contracts.DeleteDocumentRequest) error
}

type AuthenticatedDeletion struct {
	Auth      contracts.Authenticator
	Authority contracts.DeletionAuthority
	// A request-bound standard store receives controls; it never receives the
	// bearer credential. Its implementation must invoke them inside its UoW.
	Stores func(DeletionControl) (contracts.DocumentDeletionStore, error)
}

type deletionControl struct {
	auth       contracts.Authenticator
	authority  contracts.DeletionAuthority
	credential contracts.Credential
	request    contracts.DeleteDocumentRequest
	closed     atomic.Bool
}

func deletionCopy[T any](value T) (T, error) {
	var out T
	raw, e := json.Marshal(value)
	if e != nil || len(raw) > 65536 {
		return out, ErrDeletion
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, ErrDeletion
	}
	return out, nil
}

func (p *deletionControl) authenticate(ctx context.Context) (contracts.Principal, error) {
	if ctx == nil || p.closed.Load() || ctx.Err() != nil {
		return contracts.Principal{}, ErrDeletionUnauthenticated
	}
	credential := p.credential
	credential.Secret = append([]byte(nil), credential.Secret...)
	defer clear(credential.Secret)
	who, e := p.auth.Authenticate(ctx, credential)
	if e != nil || ctx.Err() != nil {
		return contracts.Principal{}, ErrDeletionUnauthenticated
	}
	who, e = deletionCopy(who)
	if e != nil {
		return contracts.Principal{}, ErrDeletionUnauthenticated
	}
	return who, nil
}

func (p *deletionControl) refresh(ctx context.Context) error {
	who, e := p.authenticate(ctx)
	if e != nil || !reflect.DeepEqual(who, p.request.Command.Principal) {
		return ErrDeletionUnauthenticated
	}
	target, e := p.authority.AuthorizeDocumentDeletion(ctx, who, p.request.DocumentID)
	target, copyErr := deletionCopy(target)
	expected := p.request.Command.Scope
	if e != nil || copyErr != nil || !validDeletionTarget(target, who, p.request.DocumentID) || target.SpaceID != expected.SpaceID || target.KnowledgeBaseID != expected.KnowledgeBaseIDs[0] || target.Scope.PolicyVersion != expected.PolicyVersion || !expected.ExpiresAt.After(time.Now()) {
		return ErrDeletionDenied
	}
	return nil
}
func (p *deletionControl) VerifyScope(ctx context.Context, s contracts.AccessScope) error {
	if !reflect.DeepEqual(s, p.request.Command.Scope) {
		return ErrDeletionDenied
	}
	return p.refresh(ctx)
}
func (p *deletionControl) AuthorizeDelete(ctx context.Context, q contracts.DeleteDocumentRequest) error {
	if !reflect.DeepEqual(q, p.request) {
		return ErrDeletionDenied
	}
	return p.refresh(ctx)
}
func validDeletionTarget(t contracts.DeletionTarget, who contracts.Principal, doc contracts.ID) bool {
	s := t.Scope
	return t.DocumentID == doc && frozenID(t.SpaceID) && frozenID(t.KnowledgeBaseID) && s.SpaceID == t.SpaceID && s.Action == "document.delete" && s.SubjectID == who.SubjectID && frozenID(s.ScopeID) && s.PolicyVersion > 0 && len(s.KnowledgeBaseIDs) == 1 && s.KnowledgeBaseIDs[0] == t.KnowledgeBaseID && s.ExpiresAt.After(time.Now())
}

func (p AuthenticatedDeletion) DeleteDocument(ctx context.Context, input AuthenticatedDeleteInput) (contracts.DeleteDocumentResult, error) {
	var empty contracts.DeleteDocumentResult
	if ctx == nil || p.Auth == nil || p.Authority == nil || p.Stores == nil || input.Request.Deadline.IsZero() || !frozenID(input.DocumentID) {
		return empty, ErrDeletion
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrDeletion
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	control := &deletionControl{auth: p.Auth, authority: p.Authority, credential: input.Credential}
	control.credential.Secret = append([]byte(nil), input.Credential.Secret...)
	defer func() { control.closed.Store(true); clear(control.credential.Secret) }()
	who, e := control.authenticate(ctx)
	if e != nil {
		return empty, e
	}
	target, e := p.Authority.AuthorizeDocumentDeletion(ctx, who, input.DocumentID)
	target, copyErr := deletionCopy(target)
	if e != nil || copyErr != nil || !validDeletionTarget(target, who, input.DocumentID) {
		return empty, ErrDeletionDenied
	}
	request := contracts.DeleteDocumentRequest{Command: contracts.CommandContext{Request: input.Request, Principal: who, Scope: target.Scope, IdempotencyKey: input.IdempotencyKey}, DocumentID: input.DocumentID, ExpectedDocumentVersion: input.ExpectedDocumentVersion}
	control.request, e = ownDeletion(request)
	if e != nil {
		return empty, e
	}
	if _, _, e = DeletionCommitments(control.request); e != nil {
		return empty, e
	}
	store, e := p.Stores(control)
	if e != nil || store == nil {
		return empty, ErrDeletion
	}
	service := DeletionService{Store: store, Verify: control.AuthorizeDelete}
	return service.Delete(ctx, request)
}

func (p *deletionControl) String() string   { return "DeletionControl([redacted])" }
func (p *deletionControl) GoString() string { return p.String() }
