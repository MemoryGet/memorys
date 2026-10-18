package cleanup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type workerAuth func(context.Context, contracts.Credential) (contracts.Principal, error)

func (f workerAuth) Authenticate(c context.Context, q contracts.Credential) (contracts.Principal, error) {
	return f(c, q)
}

type workerAuthorize func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error)

func (f workerAuthorize) Authorize(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	return f(c, q)
}

func TestWorkerAuthorityIndependentGrantsRevocationAndClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	credential := contracts.Credential{Kind: "access_token", Secret: []byte("private-worker-token")}
	who := contracts.Principal{SubjectID: "worker", SubjectType: "service", Issuer: "issuer", ClientID: "client"}
	revoked, indexAllowed, admitted := false, false, true
	policy := uint64(1)
	decisions := 0
	auth := workerAuth(func(_ context.Context, c contracts.Credential) (contracts.Principal, error) {
		if string(c.Secret) != "private-worker-token" || revoked {
			return contracts.Principal{}, errors.New("revoked")
		}
		clear(c.Secret)
		return who, nil
	})
	authorize := workerAuthorize(func(c context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
		decisions++
		if q.Action == "index.delete" && !indexAllowed {
			return contracts.AccessScope{}, errors.New("no index permission")
		}
		if len(q.ResourceIDs) > 0 && q.ResourceIDs[0] != "doc" {
			return contracts.AccessScope{}, ErrRejected
		}
		d, _ := c.Deadline()
		return contracts.AccessScope{ScopeID: contracts.ID(fmt.Sprintf("scope-%d", decisions)), SubjectID: q.Principal.SubjectID, Action: q.Action, SpaceID: q.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), q.KnowledgeBaseIDs...), PolicyVersion: policy, ExpiresAt: d}, nil
	})
	var authority *WorkerAuthority
	admission := func(c context.Context, s contracts.AccessScope, op contracts.CleanupOperation) error {
		if !admitted {
			return ErrRejected
		}
		return authority.VerifyScope(c, s)
	}
	p, e := NewWorkerAuthority(ctx, WorkerAuthorityConfig{Credential: credential, Auth: auth, Authorizer: authorize, WorkerID: "worker", SpaceID: "space", KnowledgeBaseID: "kb", VerifyOperation: admission})
	if e != nil {
		t.Fatal(e)
	}
	authority = p
	defer p.Close()
	scope, e := p.Scope(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if p.VerifyScope(ctx, scope) != nil {
		t.Fatal("current scope rejected")
	}
	forged := scope
	forged.Action = "index.delete"
	if p.VerifyScope(ctx, forged) == nil {
		t.Fatal("relabeled scope accepted")
	}
	tasks, _, _, _ := fixture()
	op, e := Operation(tasks.lease)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.IndexRequest(ctx, op); e == nil {
		t.Fatal("cleanup permission implied index permission")
	}
	indexAllowed = true
	request, e := p.IndexRequest(ctx, op)
	if e != nil || request.Scope.Action != "index.delete" || request.Scope.ScopeID == scope.ScopeID || request.GenerationID != *op.Target.GenerationID {
		t.Fatal("independent index decision missing", e)
	}
	if p.VerifyScope(ctx, request.Scope) != nil {
		t.Fatal("issued index scope rejected")
	}
	admitted = false
	if p.VerifyScope(ctx, request.Scope) == nil {
		t.Fatal("index scope outlived current operation admission")
	}
	admitted = true
	policy = 2
	if p.VerifyScope(ctx, scope) == nil || p.VerifyScope(ctx, request.Scope) == nil {
		t.Fatal("stale policy accepted")
	}
	fresh, e := p.Scope(ctx)
	if e != nil || fresh.PolicyVersion != 2 {
		t.Fatal("cycle did not renew", e)
	}
	resource := contracts.ResourceJob{ResourceID: "foreign", Job: contracts.Job{ID: "job", SpaceID: "space", ProfileDigest: contracts.Digest(strings.Repeat("a", 64))}}
	if p.AuthorizeJob(ctx, fresh, resource) == nil {
		t.Fatal("foreign resource admitted")
	}
	revoked = true
	if p.VerifyScope(ctx, fresh) == nil {
		t.Fatal("revoked service token accepted")
	}
	revoked = false
	who.ClientID = "other-client"
	if _, e = p.Scope(ctx); e == nil {
		t.Fatal("worker identity changed during session")
	}
	who.ClientID = "client"
	p.Close()
	if string(credential.Secret) != "private-worker-token" {
		t.Fatal("caller credential mutated")
	}
	for _, b := range p.credential.Secret {
		if b != 0 {
			t.Fatal("owned credential not erased")
		}
	}
	if strings.Contains(fmt.Sprintf("%#v", p), "private-worker-token") {
		t.Fatal("credential in diagnostics")
	}
	if _, e = p.Scope(ctx); e == nil {
		t.Fatal("closed worker authority reopened")
	}
}

func TestWorkerAuthorityRejectsUserAndMissingProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var empty WorkerAuthority
	if _, e := empty.Scope(ctx); e == nil {
		t.Fatal("unconfigured authority issued scope")
	}
	user := workerAuth(func(context.Context, contracts.Credential) (contracts.Principal, error) {
		return contracts.Principal{SubjectID: "worker", SubjectType: "user", Issuer: "issuer", ClientID: "client"}, nil
	})
	config := WorkerAuthorityConfig{Credential: contracts.Credential{Kind: "access_token", Secret: []byte("secret")}, Auth: user, Authorizer: workerAuthorize(func(context.Context, contracts.AuthorizeRequest) (contracts.AccessScope, error) {
		t.Fatal("user reached worker authorization")
		return contracts.AccessScope{}, nil
	}), WorkerID: "worker", SpaceID: "space", KnowledgeBaseID: "kb", VerifyOperation: func(context.Context, contracts.AccessScope, contracts.CleanupOperation) error { return nil }}
	if _, e := NewWorkerAuthority(ctx, config); e == nil {
		t.Fatal("user token enabled background worker")
	}
	var missing workerAuth
	config.Auth = missing
	if _, e := NewWorkerAuthority(ctx, config); e == nil {
		t.Fatal("typed nil authenticator accepted")
	}
}
