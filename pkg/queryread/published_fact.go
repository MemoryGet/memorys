package queryread

import (
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// PublishedCurrentFact derives a single document's fact from an already scoped
// observation, without listing other documents. It checks relationships only;
// callers still need trusted current authentication, policy and fresh reads.
func PublishedCurrentFact(scope contracts.AccessScope, identity contracts.QueryReadIdentity, state contracts.CurrentDocumentState) (contracts.PublishedFact, error) {
	var empty contracts.PublishedFact
	d := state.Document
	if d.ActiveRevisionID == nil || state.GenerationSet == nil || state.PublicationReview == nil {
		return empty, ErrInvalid
	}
	review := state.PublicationReview
	fact := contracts.PublishedFact{DocumentID: d.ID, SpaceID: d.SpaceID, KnowledgeBaseID: d.KnowledgeBaseID, RevisionID: *d.ActiveRevisionID, RowVersion: d.RowVersion, PublicationEpoch: d.PublicationEpoch, ApprovalID: review.Approval.ID, ContentDigest: review.Revision.ContentHash, GenerationSet: *state.GenerationSet}
	expected := CurrentExpectation{StoreEpochID: identity.StoreEpochID, ScopeVersion: scope.PolicyVersion, Published: []contracts.PublishedFact{fact}}
	if MatchCurrentReview(scope, expected, CurrentObservation{Identity: identity, Documents: []contracts.CurrentDocumentState{state}}, map[contracts.ID]uint64{d.KnowledgeBaseID: review.Validity.ReviewPolicyVersion}) != nil {
		return empty, ErrInvalid
	}
	raw, err := json.Marshal(fact)
	if err != nil || json.Unmarshal(raw, &empty) != nil {
		return contracts.PublishedFact{}, ErrInvalid
	}
	return empty, nil
}
