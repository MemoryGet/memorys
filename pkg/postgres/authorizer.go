package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// Authorizer resolves explicit knowledge-base grants from one current policy
// snapshot. Callers must authenticate Principal first; it is not a credential.
type Authorizer struct {
	DB            *sql.DB
	ScopeLifetime time.Duration
}

var _ contracts.Authorizer = Authorizer{}
var ErrAuthorization = errors.New("authorization denied")

func (p Authorizer) Authorize(ctx context.Context, q contracts.AuthorizeRequest) (contracts.AccessScope, error) {
	var empty contracts.AccessScope
	if ctx == nil || p.DB == nil || ctx.Err() != nil || p.ScopeLifetime < time.Second || p.ScopeLifetime > 5*time.Minute || !commandID(string(q.Principal.SubjectID)) || !commandID(string(q.Principal.ClientID)) || q.Principal.Issuer == "" || len(q.Principal.Issuer) > 2048 || (q.Principal.SubjectType != "user" && q.Principal.SubjectType != "service") || q.Principal.Delegation != nil || !commandID(string(q.SpaceID)) || !commandID(string(q.Action)) || len(q.KnowledgeBaseIDs) == 0 || len(q.KnowledgeBaseIDs) > 256 || len(q.ResourceIDs) > 1000 {
		return empty, ErrAuthorization
	}
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.After(time.Now()) {
		return empty, ErrAuthorization
	}
	kbs := map[contracts.ID]bool{}
	for _, id := range q.KnowledgeBaseIDs {
		if !commandID(string(id)) || kbs[id] {
			return empty, ErrAuthorization
		}
		kbs[id] = true
	}
	docs := map[contracts.ID]bool{}
	for _, id := range q.ResourceIDs {
		if !commandID(string(id)) || docs[id] {
			return empty, ErrAuthorization
		}
		docs[id] = true
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return empty, ErrAuthorization
	}
	defer tx.Rollback()
	var version string
	err = tx.QueryRowContext(ctx, `SELECT h.version::text FROM akos_withdraw_v1.access_policy_heads h
 JOIN akos_withdraw_v1.identity_issuers i ON i.issuer=$2 AND i.enabled
 JOIN akos_withdraw_v1.identity_subjects s ON s.issuer=i.issuer AND s.subject_id=$3 AND s.subject_type=$4 AND s.enabled
 JOIN akos_withdraw_v1.identity_clients c ON c.issuer=i.issuer AND c.client_id=$5 AND c.enabled
 WHERE h.space_id=$1`, q.SpaceID, q.Principal.Issuer, q.Principal.SubjectID, q.Principal.SubjectType, q.Principal.ClientID).Scan(&version)
	n, parseErr := strconv.ParseUint(version, 10, 64)
	if err != nil || parseErr != nil || n == 0 {
		return empty, ErrAuthorization
	}
	for _, kb := range q.KnowledgeBaseIDs {
		var allowed bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.access_policy_grants WHERE space_id=$1 AND version=$2 AND issuer=$3 AND subject_id=$4 AND client_id=$5 AND kb_id=$6 AND action=$7)`, q.SpaceID, version, q.Principal.Issuer, q.Principal.SubjectID, q.Principal.ClientID, kb, q.Action).Scan(&allowed)
		if err != nil || !allowed {
			return empty, ErrAuthorization
		}
	}
	if len(q.ResourceIDs) > 0 {
		var found int
		if tx.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.documents WHERE space_id=$1 AND id=ANY($2::text[]) AND kb_id=ANY($3::text[])`, q.SpaceID, queryKBs(q.ResourceIDs), queryKBs(q.KnowledgeBaseIDs)).Scan(&found) != nil || found != len(q.ResourceIDs) {
			return empty, ErrAuthorization
		}
	}
	if tx.Commit() != nil || ctx.Err() != nil {
		return empty, ErrAuthorization
	}
	expires := time.Now().Add(p.ScopeLifetime)
	if deadline.Before(expires) {
		expires = deadline
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return empty, ErrAuthorization
	}
	return contracts.AccessScope{ScopeID: contracts.ID(hex.EncodeToString(nonce[:])), SubjectID: q.Principal.SubjectID, Action: q.Action, SpaceID: q.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), q.KnowledgeBaseIDs...), PolicyVersion: n, ExpiresAt: expires}, nil
}
