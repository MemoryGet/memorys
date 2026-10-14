package queryread

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
)

// MatchCurrentReview adds supplied approval/revision relationships to
// MatchCurrent. reviewPolicies must come from the application's trusted current
// rule source; this function neither authenticates them nor hashes source bytes.
// A nil result is not authorization or permission to send content.
func MatchCurrentReview(scope contracts.AccessScope, expected CurrentExpectation, observed CurrentObservation, reviewPolicies map[contracts.ID]uint64) error {
	if err := MatchCurrent(scope, expected, observed); err != nil {
		return err
	}
	byID := make(map[contracts.ID]contracts.PublishedFact, len(expected.Published))
	for _, fact := range expected.Published {
		if reviewPolicies[fact.KnowledgeBaseID] == 0 {
			return ErrInvalid
		}
		byID[fact.DocumentID] = fact
	}
	for _, state := range observed.Documents {
		fact := byID[state.Document.ID]
		review := state.PublicationReview
		if review == nil {
			return ErrStale
		}
		r, a, v := review.Revision, review.Approval, review.Validity
		if r.ID != fact.RevisionID || r.DocumentID != fact.DocumentID || r.ContentHash != fact.ContentDigest || !reviewDigest(r.BlocksHash) || r.HashFormat != "akos.freeze/1" || a.ID != fact.ApprovalID || a.RevisionID != r.ID || a.ContentHash != r.ContentHash || a.Decision != "approve" || a.PolicyVersion == 0 || a.RowVersion == 0 || a.ActorID == "" || a.DecidedAt.IsZero() || v.ApprovalID != a.ID || v.ApprovalRowVersion != a.RowVersion || v.ReviewPolicyVersion != reviewPolicies[fact.KnowledgeBaseID] || v.State != "effective" {
			return ErrStale
		}
	}
	return nil
}

func reviewDigest(d contracts.Digest) bool {
	if len(d) != 64 {
		return false
	}
	for _, c := range d {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
