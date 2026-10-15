package postgres

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func reviewCommandFixture(t *testing.T) (context.Context, document.ReviewCommand, Reviews, *candidateDriverState) {
	t.Helper()
	ctx, submission, p, state := submissionFixture(t)
	c := submission.Request.Command
	c.Scope.Action = "revision.approve"
	command := document.ReviewCommand{DocumentID: "document", Request: contracts.ApprovalDecisionRequest{Command: c, RevisionID: "revision", Decision: "approve", Reason: "已核对", ExpectedApprovalVersion: 0}}
	q := contracts.ApprovalRequest{ID: "review-request", DocumentID: "document", RevisionID: "revision", SubmittedBy: "author", SubmittedAt: time.Now().Add(-time.Second), State: "submitted", RowVersion: 0, ContentHash: contracts.Digest(strings.Repeat("a", 64)), PolicyVersion: 7}
	raw, _ := json.Marshal(q)
	state.reviewRow = []driver.Value{raw, "0", "akos.freeze/1", strings.Repeat("b", 64), string(q.ContentHash), nil}
	repository := Reviews{DB: p.DB, ProfileDigest: p.ProfileDigest, ReviewPolicyVersion: 7, Verify: p.Verify, AuthorizeDecision: func(context.Context, document.ReviewCommand) error { return nil }}
	return ctx, command, repository, state
}

func TestReviewRequiresAuthorizedHumanIntentAndConfiguration(t *testing.T) {
	for _, change := range []func(*document.ReviewCommand, *Reviews){
		func(_ *document.ReviewCommand, p *Reviews) { p.AuthorizeDecision = nil },
		func(_ *document.ReviewCommand, p *Reviews) { p.ReviewPolicyVersion = 0 },
		func(c *document.ReviewCommand, _ *Reviews) { c.Request.Command.Principal.SubjectType = "service" },
		func(c *document.ReviewCommand, _ *Reviews) { c.Request.Command.Scope.Action = "document.submit" },
		func(c *document.ReviewCommand, _ *Reviews) { c.Request.Reason = string([]byte{0xff}) },
		func(c *document.ReviewCommand, _ *Reviews) { c.Request.Command.Principal.SubjectID = "other" },
	} {
		ctx, c, p, state := reviewCommandFixture(t)
		change(&c, &p)
		result, err := p.DecideReview(ctx, c)
		if !errors.Is(err, document.ErrReviewRejected) || result.ID != "" || state.begins != 0 {
			t.Fatal("unauthorized review began SQL", err)
		}
	}
}

func TestReviewCurrentAuthorityRevocationRollsBack(t *testing.T) {
	ctx, c, p, state := reviewCommandFixture(t)
	calls := 0
	p.AuthorizeDecision = func(context.Context, document.ReviewCommand) error {
		calls++
		if calls == 3 {
			return errors.New("revoked")
		}
		return nil
	}
	result, err := p.DecideReview(ctx, c)
	if !errors.Is(err, document.ErrReviewRejected) || result.ID != "" || calls != 3 || state.commits != 0 || state.rollbacks != 1 || len(state.reviewUpdate) == 0 {
		t.Fatal("review revocation not rolled back", err)
	}
}

func TestReviewCommitUnknownNeverClaimsRollback(t *testing.T) {
	ctx, c, p, state := reviewCommandFixture(t)
	state.commitErr = errors.New("lost response")
	result, err := p.DecideReview(ctx, c)
	if !errors.Is(err, document.ErrReviewUnknown) || errors.Is(err, document.ErrReviewRejected) || result.ID != "" || state.commits != 1 {
		t.Fatal("review unknown outcome misclassified", err)
	}
}

func TestReviewAuthorizersCannotRewriteDecision(t *testing.T) {
	ctx, c, p, state := reviewCommandFixture(t)
	p.Verify = func(_ context.Context, s contracts.AccessScope) error { s.KnowledgeBaseIDs[0] = "other"; return nil }
	p.AuthorizeDecision = func(_ context.Context, c document.ReviewCommand) error {
		c.Request.Command.Scope.KnowledgeBaseIDs[0] = "other"
		c.Request.Decision = "reject"
		return nil
	}
	result, err := p.DecideReview(ctx, c)
	if err != nil || result.Decision != "approve" || result.Reason != "已核对" || state.reviewUpdate[1].Value != "kb" {
		t.Fatal("authorizer rewrote request", err)
	}
}
