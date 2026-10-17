package postgres

import (
	"context"
	"database/sql"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/identitytoken"
)

// IdentityStatus checks a primary-database token allowlist and current identity
// state. It does not provision users, issue tokens or grant knowledge access.
type IdentityStatus struct{ DB *sql.DB }

var _ identitytoken.StatusSource = IdentityStatus{}

func (p IdentityStatus) Active(ctx context.Context, q identitytoken.StatusQuery) (bool, error) {
	if ctx == nil || p.DB == nil || ctx.Err() != nil || len(q.Issuer) == 0 || len(q.Issuer) > 2048 || !commandID(q.KeyID) || !commandID(q.SubjectID) || !commandID(q.ClientID) || !commandID(q.TokenID) || !commandDigest(contracts.Digest(q.PublicKeyDigest)) || (q.SubjectType != "user" && q.SubjectType != "service") || q.IssuedAt < 0 || q.ExpiresAt <= q.IssuedAt {
		return false, identitytoken.ErrAuthentication
	}
	if _, ok := ctx.Deadline(); !ok {
		return false, identitytoken.ErrAuthentication
	}
	var active bool
	err := p.DB.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM akos_withdraw_v1.identity_tokens t
 JOIN akos_withdraw_v1.identity_issuers i ON i.issuer=t.issuer
 JOIN akos_withdraw_v1.identity_signing_keys k ON k.issuer=t.issuer AND k.key_id=t.key_id
 JOIN akos_withdraw_v1.identity_subjects s ON s.issuer=t.issuer AND s.subject_id=t.subject_id
 JOIN akos_withdraw_v1.identity_clients c ON c.issuer=t.issuer AND c.client_id=t.client_id
 WHERE t.issuer=$1 AND t.key_id=$2 AND t.subject_id=$3 AND t.client_id=$4 AND t.token_id=$5
 AND s.subject_type=$6 AND k.public_key_digest=$7
 AND t.issued_at=$8 AND t.expires_at=$9
 AND i.enabled AND k.enabled AND s.enabled AND c.enabled AND NOT t.revoked
 AND t.issued_at>=k.valid_from AND t.expires_at<=k.valid_until
 AND extract(epoch FROM clock_timestamp())>=k.valid_from
 AND extract(epoch FROM clock_timestamp())<k.valid_until
 AND extract(epoch FROM clock_timestamp())<t.expires_at)`, q.Issuer, q.KeyID, q.SubjectID, q.ClientID, q.TokenID, q.SubjectType, q.PublicKeyDigest, q.IssuedAt, q.ExpiresAt).Scan(&active)
	if err != nil || ctx.Err() != nil {
		return false, identitytoken.ErrAuthentication
	}
	return active, nil
}
