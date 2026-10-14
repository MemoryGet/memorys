package queryread_test

import (
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func currentReviewFixture() (queryread.CurrentExpectation, queryread.CurrentObservation) {
	e, o := currentMatchFixture()
	f := e.Published[0]
	o.Documents[0].PublicationReview = &contracts.CurrentPublicationReview{
		Revision: contracts.FrozenRevisionFact{ID: f.RevisionID, DocumentID: f.DocumentID, ContentHash: f.ContentDigest, BlocksHash: f.ContentDigest, HashFormat: "akos.freeze/1"},
		Approval: contracts.Approval{ID: f.ApprovalID, RevisionID: f.RevisionID, ContentHash: f.ContentDigest, PolicyVersion: 2, Decision: "approve", ActorID: "reviewer", RowVersion: 3, DecidedAt: time.Now()},
		Validity: contracts.ApprovalValidityFact{ApprovalID: f.ApprovalID, ApprovalRowVersion: 3, ReviewPolicyVersion: 7, State: "effective"},
	}
	return e, o
}

func TestCurrentReviewSeparatesHistoricalReviewCurrentRulesAndACL(t *testing.T) {
	e, o := currentReviewFixture()
	// Historical review 2 remains effective under review rules 7, independently
	// of ACL policy 1. These are intentionally three different version domains.
	if err := queryread.MatchCurrentReview(currentScope(), e, o, map[contracts.ID]uint64{"kb": 7}); err != nil {
		t.Fatal(err)
	}
	if queryread.MatchCurrentReview(currentScope(), e, o, nil) != queryread.ErrInvalid {
		t.Fatal("missing current review policy accepted")
	}
	if queryread.MatchCurrentReview(currentScope(), e, o, map[contracts.ID]uint64{"kb": 8}) != queryread.ErrStale {
		t.Fatal("old rule evaluation accepted")
	}
	o.Documents[0].Document.PublicationEpoch++
	if queryread.MatchCurrentReview(currentScope(), e, o, map[contracts.ID]uint64{"kb": 7}) != queryread.ErrStale {
		t.Fatal("approval bypassed publication mismatch")
	}
}

func TestCurrentReviewRejectsMissingRevokedAndMismatchedFacts(t *testing.T) {
	cases := map[string]func(*contracts.CurrentPublicationReview){
		"wrong revision":          func(r *contracts.CurrentPublicationReview) { r.Revision.ID = "other" },
		"wrong document":          func(r *contracts.CurrentPublicationReview) { r.Revision.DocumentID = "other" },
		"content changed":         func(r *contracts.CurrentPublicationReview) { r.Revision.ContentHash = "bad" },
		"blocks absent":           func(r *contracts.CurrentPublicationReview) { r.Revision.BlocksHash = "" },
		"unsupported freeze":      func(r *contracts.CurrentPublicationReview) { r.Revision.HashFormat = "vendor/1" },
		"wrong approval":          func(r *contracts.CurrentPublicationReview) { r.Approval.ID = "other" },
		"approval revision":       func(r *contracts.CurrentPublicationReview) { r.Approval.RevisionID = "other" },
		"approval content":        func(r *contracts.CurrentPublicationReview) { r.Approval.ContentHash = "bad" },
		"revoked":                 func(r *contracts.CurrentPublicationReview) { r.Approval.Decision = "revoke" },
		"wrong enum":              func(r *contracts.CurrentPublicationReview) { r.Approval.Decision = "approved" },
		"no policy":               func(r *contracts.CurrentPublicationReview) { r.Approval.PolicyVersion = 0 },
		"no version":              func(r *contracts.CurrentPublicationReview) { r.Approval.RowVersion = 0 },
		"no actor":                func(r *contracts.CurrentPublicationReview) { r.Approval.ActorID = "" },
		"no decision time":        func(r *contracts.CurrentPublicationReview) { r.Approval.DecidedAt = time.Time{} },
		"wrong validity approval": func(r *contracts.CurrentPublicationReview) { r.Validity.ApprovalID = "other" },
		"old decision evaluation": func(r *contracts.CurrentPublicationReview) { r.Validity.ApprovalRowVersion-- },
		"revoked validity":        func(r *contracts.CurrentPublicationReview) { r.Validity.State = "revoked" },
		"unknown validity":        func(r *contracts.CurrentPublicationReview) { r.Validity.State = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e, o := currentReviewFixture()
			mutate(o.Documents[0].PublicationReview)
			if queryread.MatchCurrentReview(currentScope(), e, o, map[contracts.ID]uint64{"kb": 7}) != queryread.ErrStale {
				t.Fatal("invalid review accepted")
			}
		})
	}
	e, o := currentReviewFixture()
	o.Documents[0].PublicationReview = nil
	if queryread.MatchCurrentReview(currentScope(), e, o, map[contracts.ID]uint64{"kb": 7}) != queryread.ErrStale {
		t.Fatal("legacy missing projection interpreted as valid")
	}
}
