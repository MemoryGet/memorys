package postgres

import (
	"context"
	"database/sql"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"unicode/utf8"
)

// ListKnowledgeBases resolves the omitted KB-list case from current explicit
// grants. Principal must already be authenticated by the application.
func (p Authorizer) ListKnowledgeBases(ctx context.Context, who contracts.Principal, space contracts.ID) ([]contracts.ID, error) {
	if ctx == nil || p.DB == nil || ctx.Err() != nil || !commandID(string(space)) || !commandID(string(who.SubjectID)) || !commandID(string(who.ClientID)) || who.Issuer == "" || len(who.Issuer) > 2048 || !utf8.ValidString(who.Issuer) || (who.SubjectType != "user" && who.SubjectType != "service") || who.Delegation != nil {
		return nil, ErrAuthorization
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrAuthorization
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, ErrAuthorization
	}
	defer tx.Rollback()
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM akos_withdraw_v1.identity_issuers i
 JOIN akos_withdraw_v1.identity_subjects s ON s.issuer=i.issuer AND s.subject_id=$3 AND s.subject_type=$4 AND s.enabled
 JOIN akos_withdraw_v1.identity_clients c ON c.issuer=i.issuer AND c.client_id=$5 AND c.enabled
 JOIN akos_withdraw_v1.access_policy_heads h ON h.space_id=$1
 WHERE i.issuer=$2 AND i.enabled)`, space, who.Issuer, who.SubjectID, who.SubjectType, who.ClientID).Scan(&active)
	if err != nil || !active {
		return nil, ErrAuthorization
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT g.kb_id FROM akos_withdraw_v1.access_policy_heads h
 JOIN akos_withdraw_v1.access_policy_grants g ON g.space_id=h.space_id AND g.version=h.version
 JOIN akos_withdraw_v1.identity_issuers i ON i.issuer=g.issuer AND i.enabled
 JOIN akos_withdraw_v1.identity_subjects s ON s.issuer=g.issuer AND s.subject_id=g.subject_id AND s.enabled
 JOIN akos_withdraw_v1.identity_clients c ON c.issuer=g.issuer AND c.client_id=g.client_id AND c.enabled
 WHERE h.space_id=$1 AND g.issuer=$2 AND g.subject_id=$3 AND s.subject_type=$4 AND g.client_id=$5 AND g.action='knowledge.search'
 ORDER BY g.kb_id LIMIT 257`, space, who.Issuer, who.SubjectID, who.SubjectType, who.ClientID)
	if err != nil {
		return nil, ErrAuthorization
	}
	defer rows.Close()
	out := []contracts.ID{}
	for rows.Next() {
		var id contracts.ID
		if rows.Scan(&id) != nil || !commandID(string(id)) {
			return nil, ErrAuthorization
		}
		out = append(out, id)
	}
	if rows.Err() != nil || ctx.Err() != nil || len(out) > 256 {
		return nil, ErrAuthorization
	}
	if rows.Close() != nil || tx.Commit() != nil || ctx.Err() != nil {
		return nil, ErrAuthorization
	}
	return out, nil
}
