package queryread

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// MatchRefreshedScope checks supplied identity/scope relationships only. Both
// principals and scopes must already be authenticated by the application. The
// original values must be saved independently of the refreshed response.
// Credential/proof references may rotate; actor, delegation target and range
// cannot change. It does not verify credentials, revocation or control sources.
func MatchRefreshedScope(ctx context.Context, original contracts.Principal, before contracts.AccessScope, refreshed contracts.Principal, current contracts.AccessScope, requiredKBs []contracts.ID) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || !deadline.After(time.Now()) || !principalShape(original) || !principalShape(refreshed) || len(requiredKBs) > 256 {
		return ErrInvalid
	}
	oldKBs, ok := scopeKBs(before, original)
	if !ok {
		return ErrInvalid
	}
	newKBs, ok := scopeKBs(current, refreshed)
	if !ok {
		return ErrInvalid
	}
	if original.SubjectID != refreshed.SubjectID || original.SubjectType != refreshed.SubjectType || original.Issuer != refreshed.Issuer || original.ClientID != refreshed.ClientID || before.SubjectID != current.SubjectID || before.SpaceID != current.SpaceID || before.Action != current.Action || before.PolicyVersion != current.PolicyVersion || !current.ExpiresAt.After(time.Now()) {
		return ErrStale
	}
	a, b := original.Delegation, refreshed.Delegation
	if (a == nil) != (b == nil) || a != nil && (a.UserID != b.UserID || a.ClientID != b.ClientID) {
		return ErrStale
	}
	for kb := range newKBs {
		if !oldKBs[kb] {
			return ErrStale
		}
	}
	seen := make(map[contracts.ID]bool, len(requiredKBs))
	for _, kb := range requiredKBs {
		if !scopeText(string(kb)) || seen[kb] || !oldKBs[kb] {
			return ErrInvalid
		}
		seen[kb] = true
		if !newKBs[kb] {
			return ErrStale
		}
	}
	return ctx.Err()
}

func principalShape(p contracts.Principal) bool {
	if !scopeText(string(p.SubjectID)) || !scopeText(p.Issuer) || p.SubjectType != "user" && p.SubjectType != "service" || p.ClientID != "" && !scopeText(string(p.ClientID)) {
		return false
	}
	d := p.Delegation
	return d == nil || scopeText(string(d.UserID)) && scopeText(string(d.ClientID)) && scopeText(d.ProofReference)
}

func scopeKBs(s contracts.AccessScope, p contracts.Principal) (map[contracts.ID]bool, bool) {
	if !scopeText(string(s.ScopeID)) || s.SubjectID != p.SubjectID || !scopeText(string(s.SpaceID)) || !contracts.IsCurrentContentReadAction(s.Action) || s.PolicyVersion == 0 || s.ExpiresAt.IsZero() || len(s.KnowledgeBaseIDs) == 0 || len(s.KnowledgeBaseIDs) > 256 {
		return nil, false
	}
	result := make(map[contracts.ID]bool, len(s.KnowledgeBaseIDs))
	for _, kb := range s.KnowledgeBaseIDs {
		if !scopeText(string(kb)) || result[kb] {
			return nil, false
		}
		result[kb] = true
	}
	return result, true
}

func scopeText(s string) bool {
	return len(s) > 0 && len(s) <= 4096 && utf8.ValidString(s)
}
