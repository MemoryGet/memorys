package publicationprojection

import (
	"slices"
	"sort"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// CallIdentity must come from the frozen application plan and invocation, not
// the backend response. It is a binding expectation, not authentication.
type CallIdentity struct {
	InvocationID, ModuleInstanceID contracts.ID
	Limit                          int
}

// ValidateRequest checks a given projection against the actual request. It
// neither derives authority nor repairs/filters a mismatched request.
func ValidateRequest(raw []byte, expected CallIdentity, q contracts.RetrieveRequest) error {
	r, digest, err := ValidatedDigest(raw)
	if err != nil {
		return ErrInvalid
	}
	if expected.InvocationID != r.InvocationID || expected.ModuleInstanceID != r.ModuleInstanceID || expected.Limit < 1 || expected.Limit > 1000 || q.Limit != expected.Limit || q.Request.RequestID != r.RequestID || q.Request.ProfileDigest != r.PipelineDigest || q.RouteID != r.RouteID || q.BackendEpochID != r.BackendEpochID {
		return ErrInvalid
	}
	expiry := time.Unix(0, r.ExpiresAtUnixNS)
	if q.Request.Deadline.IsZero() || q.Request.Deadline.After(expiry) || q.Scope.ExpiresAt.Before(expiry) {
		return ErrInvalid
	}
	if q.Scope.ScopeID != r.ChildScopeID || !validID(q.Scope.SubjectID) || q.Scope.Action != "knowledge.search" || q.Scope.SpaceID != r.SpaceID || q.Scope.PolicyVersion != r.PolicyVersion || !slices.Equal(q.Scope.KnowledgeBaseIDs, []contracts.ID{r.KnowledgeBaseID}) {
		return ErrInvalid
	}
	f := q.Publication
	if f.SpaceID != r.SpaceID || !slices.Equal(f.KnowledgeBaseIDs, []contracts.ID{r.KnowledgeBaseID}) || f.SnapshotDigest != digest || !slices.Equal(q.Channels, r.Channels) || len(q.Generations) != len(r.Entries) {
		return ErrInvalid
	}
	entries := map[contracts.ID]Entry{}
	revisions := map[contracts.ID]bool{}
	var genIDs []contracts.ID
	for _, entry := range r.Entries {
		entries[entry.GenerationID] = entry
		revisions[entry.RevisionID] = true
		genIDs = append(genIDs, entry.GenerationID)
	}
	var revIDs []contracts.ID
	for id := range revisions {
		revIDs = append(revIDs, id)
	}
	sort.Slice(revIDs, func(i, j int) bool { return revIDs[i] < revIDs[j] })
	sort.Slice(genIDs, func(i, j int) bool { return genIDs[i] < genIDs[j] })
	if !slices.Equal(f.RevisionIDs, revIDs) || !slices.Equal(f.GenerationIDs, genIDs) {
		return ErrInvalid
	}
	seen := map[contracts.ID]bool{}
	for _, g := range q.Generations {
		e, ok := entries[g.ID]
		if !ok || seen[g.ID] || g.KnowledgeBaseID != r.KnowledgeBaseID || g.BackendEpochID != r.BackendEpochID || g.RevisionID != e.RevisionID || g.Channel != e.Channel || g.ProfileDigest != e.IndexProfileDigest || g.State != "ready" || g.Readiness == nil || g.Readiness.GenerationSetID != e.GenerationSetID {
			return ErrInvalid
		}
		seen[g.ID] = true
	}
	return nil
}
