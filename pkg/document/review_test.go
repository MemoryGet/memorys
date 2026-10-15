package document_test

import (
	"math"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func reviewFixture(t *testing.T) (contracts.ApprovalRequest, contracts.FrozenRevisionFact, time.Time) {
	t.Helper()
	at := time.Now().UTC()
	p, err := document.PrepareSubmission(frozenDraftFixture(), 1, "revision", "review-request", "author", 7, at.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if p.Approval.State != "submitted" || p.Approval.RowVersion != 0 {
		t.Fatal("initial review differs from management contract")
	}
	return p.Approval, contracts.FrozenRevisionFact{ID: p.Revision.ID, DocumentID: p.Revision.DocumentID, ContentHash: p.Revision.ContentHash, BlocksHash: p.Revision.BlocksHash, HashFormat: p.Revision.HashFormat}, at
}
func TestReviewDecisionBindsContentAndKeepsHistory(t *testing.T) {
	q, f, at := reviewFixture(t)
	approved, err := document.PrepareReview(q, f, 0, "decision-approve", "reviewer", "approve", "核对内容", 7, at)
	if err != nil || approved.Request.RowVersion != 1 || approved.Request.State != "approved" || approved.Decision.ContentHash != f.ContentHash {
		t.Fatal("approve failed", err)
	}
	revoked, err := document.PrepareReview(approved.Request, f, 1, "decision-revoke", "reviewer", "revoke", "依据已过时", 8, at.Add(time.Second))
	if err != nil || revoked.Request.RowVersion != 2 || revoked.Request.State != "revoked" || revoked.Decision.PolicyVersion != 8 || approved.Decision.Decision != "approve" || q.State != "submitted" {
		t.Fatal("revocation changed history", err)
	}
	if _, err := document.PrepareReview(revoked.Request, f, 2, "again", "reviewer", "approve", "", 8, at.Add(2*time.Second)); err == nil {
		t.Fatal("revoked version silently reapproved")
	}
}
func TestReviewRejectsStaleHashVersionStateAndClock(t *testing.T) {
	for _, change := range []func(*contracts.ApprovalRequest, *contracts.FrozenRevisionFact, *time.Time){
		func(q *contracts.ApprovalRequest, _ *contracts.FrozenRevisionFact, _ *time.Time) { q.RowVersion = 1 },
		func(q *contracts.ApprovalRequest, _ *contracts.FrozenRevisionFact, _ *time.Time) {
			q.State = "rejected"
		},
		func(q *contracts.ApprovalRequest, _ *contracts.FrozenRevisionFact, _ *time.Time) {
			q.ContentHash = "wrong"
		},
		func(_ *contracts.ApprovalRequest, f *contracts.FrozenRevisionFact, _ *time.Time) {
			f.DocumentID = "other"
		},
		func(_ *contracts.ApprovalRequest, f *contracts.FrozenRevisionFact, _ *time.Time) { f.BlocksHash = "" },
		func(q *contracts.ApprovalRequest, _ *contracts.FrozenRevisionFact, at *time.Time) {
			*at = q.SubmittedAt.Add(-time.Second)
		},
	} {
		q, f, at := reviewFixture(t)
		change(&q, &f, &at)
		if _, err := document.PrepareReview(q, f, 0, "decision", "reviewer", "approve", "", 7, at); err == nil {
			t.Fatal("invalid review accepted")
		}
	}
	q, f, at := reviewFixture(t)
	if _, err := document.PrepareReview(q, f, 0, "decision", "reviewer", "revoke", "", 7, at); err == nil {
		t.Fatal("unapproved revision revoked")
	}
	q.State = "approved"
	q.RowVersion = math.MaxUint64
	if _, err := document.PrepareReview(q, f, math.MaxUint64, "decision", "reviewer", "revoke", "", 7, at); err == nil {
		t.Fatal("review version overflow accepted")
	}
}
