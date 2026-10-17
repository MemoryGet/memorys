package postgres

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// QueryFacts reads active pipeline references and review policy heads. It does
// not compile profiles or establish the caller's identity/authorization.
type QueryFacts struct{ DB *sql.DB }

var _ queryread.CurrentFactsSource = QueryFacts{}

func (p QueryFacts) LoadCurrentFacts(ctx context.Context, r contracts.RequestContext, s contracts.AccessScope) (queryread.ControlFacts, error) {
	var empty queryread.ControlFacts
	if ctx == nil || p.DB == nil || ctx.Err() != nil || !commandID(string(r.RequestID)) || !commandDigest(r.ProfileDigest) || r.Deadline.IsZero() || !commandID(string(s.SubjectID)) || !commandID(string(s.ScopeID)) || !commandID(string(s.SpaceID)) || !contracts.IsCurrentContentReadAction(s.Action) || s.PolicyVersion == 0 || !s.ExpiresAt.After(time.Now()) || len(s.KnowledgeBaseIDs) == 0 || len(s.KnowledgeBaseIDs) > 256 {
		return empty, queryread.ErrCheck
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, queryread.ErrCheck
	}
	ctx, cancel := context.WithDeadline(ctx, r.Deadline)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, s.ExpiresAt)
	defer expire()
	seen := map[contracts.ID]bool{}
	for _, kb := range s.KnowledgeBaseIDs {
		if !commandID(string(kb)) || seen[kb] {
			return empty, queryread.ErrCheck
		}
		seen[kb] = true
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return empty, queryread.ErrCheck
	}
	defer tx.Rollback()
	out := queryread.ControlFacts{ReviewPolicies: map[contracts.ID]uint64{}}
	for _, kb := range s.KnowledgeBaseIDs {
		var digest contracts.Digest
		var review string
		var payload []byte
		err = tx.QueryRowContext(ctx, `SELECT b.profile_digest,r.policy_version::text,c.payload
 FROM akos_withdraw_v1.query_profile_heads h
 JOIN akos_withdraw_v1.query_profile_bindings b ON b.space_id=h.space_id AND b.kb_id=h.kb_id AND b.version=h.version
 JOIN akos_withdraw_v1.query_profile_catalog c ON c.profile_digest=b.profile_digest
 JOIN akos_withdraw_v1.review_policy_heads r ON r.space_id=h.space_id AND r.kb_id=h.kb_id
 JOIN akos_withdraw_v1.access_policy_heads a ON a.space_id=h.space_id AND a.version=$3
 WHERE h.space_id=$1 AND h.kb_id=$2`, s.SpaceID, kb, strconv.FormatUint(s.PolicyVersion, 10)).Scan(&digest, &review, &payload)
		v, parseErr := strconv.ParseUint(review, 10, 64)
		if err != nil || parseErr != nil || v == 0 || !commandDigest(digest) || (out.ProfileDigest != "" && out.ProfileDigest != digest) {
			return empty, queryread.ErrCheck
		}
		snapshot, verifyErr := configprofile.Decode(payload)
		if verifyErr != nil || snapshot.Pipeline.Digest != digest {
			return empty, queryread.ErrCheck
		}
		out.ProfileDigest = digest
		out.ReviewPolicies[kb] = v
	}
	if tx.Commit() != nil || ctx.Err() != nil {
		return empty, queryread.ErrCheck
	}
	return out, nil
}
