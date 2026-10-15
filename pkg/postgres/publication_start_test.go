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

func publicationStartFixture(t *testing.T) (context.Context, contracts.PublishRequest, PublicationStarts, *candidateDriverState) {
	t.Helper()
	ctx, base, p, state := reviewCommandFixture(t)
	c := base.Request.Command
	c.Scope.Action = "publication.start"
	r := contracts.PublishRequest{Command: c, DocumentID: "document", RevisionID: "revision", ApprovalID: "decision", ExpectedEpoch: 0}
	request := contracts.ApprovalRequest{ID: "review-request", DocumentID: r.DocumentID, RevisionID: r.RevisionID, State: "approved", RowVersion: 1, ContentHash: contracts.Digest(strings.Repeat("a", 64)), PolicyVersion: 7, SubmittedBy: "author", SubmittedAt: time.Now().Add(-time.Second)}
	approval := contracts.Approval{ID: "decision", RevisionID: r.RevisionID, ContentHash: request.ContentHash, Decision: "approve", PolicyVersion: 7, RowVersion: 1, ActorID: "reviewer", DecidedAt: time.Now()}
	qBytes, _ := json.Marshal(request)
	state.publicationDecision, _ = json.Marshal(approval)
	state.publicationDoc = []driver.Value{"0", "1", nil, nil, false}
	state.publicationReview = []driver.Value{qBytes, "decision", "akos.freeze/1", strings.Repeat("b", 64), string(request.ContentHash)}
	policy := document.ReviewPolicySnapshot{Version: 9, AcceptedDecisionPolicyVersions: []uint64{7}}
	policy.Digest, _ = document.ReviewPolicyDigest(policy)
	policyBytes, _ := json.Marshal(policy)
	state.publicationPolicy = []driver.Value{"9", "1", string(policy.Digest), policyBytes}
	backend := document.PublicationBackendSnapshot{EpochID: "backend", Number: 1, CutoverWatermark: "backend-watermark", IndexProfile: contracts.ProfileReference{ID: "index-profile", Version: 1}, ProfileDigest: contracts.Digest(strings.Repeat("d", 64)), RequiredChannels: []contracts.Channel{contracts.KeywordChannel}}
	backendBytes, _ := json.Marshal(backend)
	state.publicationBackend = []driver.Value{"1", "backend", backendBytes}
	repository := PublicationStarts{DB: p.DB, ProfileDigest: p.ProfileDigest, Verify: p.Verify, AuthorizeStart: func(context.Context, contracts.PublishRequest) error { return nil }}
	return ctx, r, repository, state
}
func TestPublicationStartRequiresAuthorityAndCurrentHead(t *testing.T) {
	for _, change := range []func(*PublicationStarts, *candidateDriverState){
		func(p *PublicationStarts, _ *candidateDriverState) { p.AuthorizeStart = nil },
		func(_ *PublicationStarts, s *candidateDriverState) { s.publicationReview[1] = "revoked-decision" },
		func(_ *PublicationStarts, s *candidateDriverState) { s.publicationPolicy[2] = strings.Repeat("f", 64) },
	} {
		ctx, r, p, state := publicationStartFixture(t)
		change(&p, state)
		result, err := p.StartPublication(ctx, r)
		if !errors.Is(err, document.ErrPublicationStartRejected) || result.Operation.ID != "" || state.commits != 0 || len(state.publicationInsert) != 0 {
			t.Fatal("invalid publication intent persisted", err)
		}
	}
}
func TestPublicationStartPrecommitAuthorityAndCommitOutcome(t *testing.T) {
	t.Run("revoked", func(t *testing.T) {
		ctx, r, p, state := publicationStartFixture(t)
		calls := 0
		p.AuthorizeStart = func(context.Context, contracts.PublishRequest) error {
			calls++
			if calls == 3 {
				return errors.New("revoked")
			}
			return nil
		}
		result, err := p.StartPublication(ctx, r)
		if !errors.Is(err, document.ErrPublicationStartRejected) || result.Operation.ID != "" || len(state.publicationInsert) == 0 || state.commits != 0 || state.rollbacks != 1 {
			t.Fatal("publication revocation not rolled back", err)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		ctx, r, p, state := publicationStartFixture(t)
		state.commitErr = errors.New("lost commit response")
		result, err := p.StartPublication(ctx, r)
		if !errors.Is(err, document.ErrPublicationStartUnknown) || errors.Is(err, document.ErrPublicationStartRejected) || result.Operation.ID != "" || state.commits != 1 {
			t.Fatal("publication uncertainty classified as rollback", err)
		}
	})
}
func TestPublicationStartReturnsPreparationWithoutHeadChange(t *testing.T) {
	ctx, r, p, state := publicationStartFixture(t)
	result, err := p.StartPublication(ctx, r)
	if err != nil || result.Operation.State != "preparing" || result.Operation.GenerationSetID != nil || result.Job.State != "queued" || len(state.update) != 0 {
		t.Fatal("start pretended to complete publication", err)
	}
}
