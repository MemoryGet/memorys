package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func submissionFixture(t *testing.T) (context.Context, document.SubmissionCommand, Submissions, *candidateDriverState) {
	t.Helper()
	ctx, _, candidate, db, state := candidateCommandFixture(t)
	var draft contracts.Draft
	if json.Unmarshal(state.draft, &draft) != nil {
		t.Fatal("fixture draft decode")
	}
	draft.Blobs[0].MediaType = "text/plain"
	for _, block := range candidate.Candidate.Blocks {
		draft.Blocks = append(draft.Blocks, contracts.DraftBlock{ID: block.ID, DraftID: draft.ID, RowVersion: 1, Order: block.Order, Content: block.Content})
	}
	state.draft, _ = json.Marshal(draft)
	scope := candidate.Scope
	scope.Action = "document.submit"
	command := document.SubmissionCommand{DocumentID: "document", Request: contracts.SubmitRequest{DraftID: "draft", ExpectedVersion: 1, Command: contracts.CommandContext{Request: candidate.Request, Scope: scope, Principal: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "fixture"}, IdempotencyKey: "submit-key"}}}
	repository := Submissions{DB: db, ReviewPolicyVersion: 7, ProfileDigest: candidate.Request.ProfileDigest, Verify: func(context.Context, contracts.AccessScope) error { return nil }, AuthorizeSubmission: func(context.Context, document.SubmissionCommand) error { return nil }, VerifyContent: func(context.Context, contracts.AccessScope, contracts.Draft) error { return nil }}
	return ctx, command, repository, state
}

func TestSubmissionRequiresTrustedContentAndResourceChecksBeforeSQL(t *testing.T) {
	for _, change := range []func(*document.SubmissionCommand, *Submissions){
		func(_ *document.SubmissionCommand, p *Submissions) { p.VerifyContent = nil },
		func(_ *document.SubmissionCommand, p *Submissions) { p.AuthorizeSubmission = nil },
		func(_ *document.SubmissionCommand, p *Submissions) { p.ReviewPolicyVersion = 0 },
		func(c *document.SubmissionCommand, _ *Submissions) { c.Request.Command.Principal.SubjectID = "other" },
		func(c *document.SubmissionCommand, _ *Submissions) {
			c.Request.Command.IdempotencyKey = string([]byte{0xff})
		},
		func(c *document.SubmissionCommand, _ *Submissions) { c.Request.Command.Request.ProfileDigest = "" },
	} {
		ctx, c, p, state := submissionFixture(t)
		change(&c, &p)
		result, err := p.SubmitDraft(ctx, c)
		if !errors.Is(err, document.ErrSubmissionRejected) || result.Revision.ID != "" || state.begins != 0 {
			t.Fatal("invalid submission began SQL", err)
		}
	}
}

func TestSubmissionCallbacksCannotRewriteLockedContentOrScope(t *testing.T) {
	ctx, c, p, state := submissionFixture(t)
	p.Verify = func(_ context.Context, s contracts.AccessScope) error { s.KnowledgeBaseIDs[0] = "changed"; return nil }
	p.AuthorizeSubmission = func(_ context.Context, c document.SubmissionCommand) error {
		c.Request.Command.Scope.KnowledgeBaseIDs[0] = "changed"
		return nil
	}
	p.VerifyContent = func(_ context.Context, s contracts.AccessScope, d contracts.Draft) error {
		s.KnowledgeBaseIDs[0] = "changed"
		d.Blobs[0].MediaType = "changed"
		d.Blocks[0].Content.Text = "changed"
		return nil
	}
	result, err := p.SubmitDraft(ctx, c)
	if err != nil || result.Revision.Blocks[0].Content.Text != "candidate" || result.Revision.Blobs[0].MediaType != "text/plain" || state.commits != 1 || state.update[1].Value != "kb" {
		t.Fatal("callback rewrote authoritative input", err)
	}
}

func TestSubmissionRechecksAuthorityAndClassifiesUncertainCommit(t *testing.T) {
	t.Run("revoked", func(t *testing.T) {
		ctx, c, p, state := submissionFixture(t)
		calls := 0
		p.AuthorizeSubmission = func(context.Context, document.SubmissionCommand) error {
			calls++
			if calls == 4 {
				return errors.New("revoked")
			}
			return nil
		}
		result, err := p.SubmitDraft(ctx, c)
		if !errors.Is(err, document.ErrSubmissionRejected) || result.Revision.ID != "" || state.commits != 0 || state.rollbacks != 1 || len(state.update) == 0 {
			t.Fatal("revoked submission committed", err)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		ctx, c, p, state := submissionFixture(t)
		state.commitErr = errors.New("driver lost commit response")
		result, err := p.SubmitDraft(ctx, c)
		if !errors.Is(err, document.ErrSubmissionUnknown) || errors.Is(err, document.ErrSubmissionRejected) || result.Revision.ID != "" || state.commits != 1 {
			t.Fatal("unknown commit falsely proved rollback", err)
		}
	})
}

func TestSubmissionVerifiesOriginalOutsideTransactionAndPinsExactSnapshot(t *testing.T) {
	ctx, c, p, state := submissionFixture(t)
	p.VerifyContent = func(_ context.Context, _ contracts.AccessScope, _ contracts.Draft) error {
		if state.begins != 0 {
			t.Fatal("blob I/O held a database transaction")
		}
		// Even an invalid writer that forgets to increase the version cannot
		// exchange content after original verification and before the row lock.
		var changed contracts.Draft
		if json.Unmarshal(state.draft, &changed) != nil {
			t.Fatal("fixture decode")
		}
		changed.Blocks[0].Content.Text = "replaced between verification and locking"
		state.draft, _ = json.Marshal(changed)
		return nil
	}
	result, err := p.SubmitDraft(ctx, c)
	if !errors.Is(err, document.ErrSubmissionRejected) || result.Revision.ID != "" || state.commits != 0 || len(state.update) != 0 {
		t.Fatal("unverified snapshot committed", err)
	}
}
