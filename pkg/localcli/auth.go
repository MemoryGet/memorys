package localcli

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/identitytoken"
	"github.com/MemoryGet/memorys/pkg/postgres"
	jose "github.com/go-jose/go-jose/v4"
)

func profileBytes(p configprofile.Snapshot) ([]byte, error) { return configprofile.Encode(p) }

var errAuthority = errors.New("current local identity or permission denied")

type authority struct {
	s          *state
	db         *sql.DB
	auth       contracts.Authenticator
	credential contracts.Credential
	principal  contracts.Principal
}

// Every operation gets a newly registered, bounded token. Disabled identities
// and keys are never re-enabled; registration itself is not authentication.
func (s *state) identity(ctx context.Context, db *sql.DB, worker bool) (*authority, error) {
	if s.key == nil {
		return nil, errState
	}
	sub, kind := s.m.Owner, "user"
	if worker {
		sub, kind = s.m.Worker, "service"
	}
	now := time.Now().Unix()
	exp := now + 900
	if now < s.m.KeyFrom || exp > s.m.KeyUntil {
		return nil, errAuthority
	}
	id := nonce()
	if _, err := db.ExecContext(ctx, `INSERT INTO akos_withdraw_v1.identity_tokens VALUES($1,$2,$3,$4,$5,$6,$7,false)`, s.m.Issuer, id, s.m.KeyID, sub, s.m.Client, now, exp); err != nil {
		return nil, errAuthority
	}
	opts := (&jose.SignerOptions{}).WithType("at+jwt").WithHeader("kid", string(s.m.KeyID))
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: s.key}, opts)
	if err != nil {
		return nil, errAuthority
	}
	raw, _ := json.Marshal(map[string]any{"iss": s.m.Issuer, "aud": s.m.Audience, "sub": sub, "client_id": s.m.Client, "jti": id, "iat": now, "nbf": now, "exp": exp})
	signed, err := signer.Sign(raw)
	if err != nil {
		return nil, errAuthority
	}
	token, err := signed.CompactSerialize()
	if err != nil {
		return nil, errAuthority
	}
	auth, err := identitytoken.New(identitytoken.Config{Issuer: s.m.Issuer, Audience: s.m.Audience, SubjectType: kind, Keys: map[string]*rsa.PublicKey{string(s.m.KeyID): &s.key.PublicKey}, MaxLifetime: 15 * time.Minute}, postgres.IdentityStatus{DB: db})
	if err != nil {
		return nil, errAuthority
	}
	a := &authority{s: s, db: db, auth: auth, credential: contracts.Credential{Kind: "access_token", Secret: []byte(token)}}
	a.principal, err = a.auth.Authenticate(ctx, a.credential)
	if err != nil {
		a.close()
		return nil, errAuthority
	}
	return a, nil
}
func (a *authority) close() { clear(a.credential.Secret) }
func (a *authority) scope(ctx context.Context, action contracts.Action, doc contracts.ID) (contracts.AccessScope, error) {
	p, err := a.auth.Authenticate(ctx, a.credential)
	if err != nil || !reflect.DeepEqual(p, a.principal) {
		return contracts.AccessScope{}, errAuthority
	}
	q := contracts.AuthorizeRequest{Principal: p, Action: action, SpaceID: a.s.m.Space, KnowledgeBaseIDs: []contracts.ID{a.s.m.KB}}
	if doc != "" {
		q.ResourceIDs = []contracts.ID{doc}
	}
	return (postgres.Authorizer{DB: a.db, ScopeLifetime: time.Minute}).Authorize(ctx, q)
}
func (a *authority) verify(ctx context.Context, before contracts.AccessScope) error {
	fresh, err := a.scope(ctx, before.Action, "")
	if err != nil || fresh.SubjectID != before.SubjectID || fresh.PolicyVersion != before.PolicyVersion || fresh.SpaceID != before.SpaceID || !reflect.DeepEqual(fresh.KnowledgeBaseIDs, before.KnowledgeBaseIDs) || !before.ExpiresAt.After(time.Now()) {
		return errAuthority
	}
	return nil
}
func (a *authority) command(ctx context.Context, action contracts.Action, doc contracts.ID, key string) (contracts.CommandContext, error) {
	scope, err := a.scope(ctx, action, doc)
	if err != nil {
		return contracts.CommandContext{}, errAuthority
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return contracts.CommandContext{}, errAuthority
	}
	return contracts.CommandContext{Request: contracts.RequestContext{RequestID: contracts.ID(nonce()), ProfileDigest: a.s.m.ProfileDigest, Deadline: deadline}, Principal: a.principal, Scope: scope, IdempotencyKey: key}, nil
}
func (a *authority) checkCommand(ctx context.Context, c contracts.CommandContext, action contracts.Action, doc contracts.ID) error {
	if !reflect.DeepEqual(c.Principal, a.principal) || c.Scope.Action != action || c.Request.ProfileDigest != a.s.m.ProfileDigest || a.verify(ctx, c.Scope) != nil {
		return errAuthority
	}
	_, err := a.scope(ctx, action, doc)
	return err
}
