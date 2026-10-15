package document_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

func publicationFixture(t *testing.T) (contracts.PublishRequest, contracts.Document, contracts.CurrentPublicationReview, document.ReviewPolicySnapshot, document.PublicationBackendSnapshot) {
	t.Helper()
	q, f, at := reviewFixture(t)
	prepared, err := document.PrepareReview(q, f, 0, "approved", "reviewer", "approve", "", 7, at)
	if err != nil {
		t.Fatal(err)
	}
	policy := document.ReviewPolicySnapshot{Version: 9, AcceptedDecisionPolicyVersions: []uint64{7}}
	policy.Digest, err = document.ReviewPolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	view, err := document.CurrentApproval(f, prepared.Request, prepared.Decision.ID, prepared.Decision, prepared.Decision.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	r := contracts.PublishRequest{DocumentID: f.DocumentID, RevisionID: f.ID, ApprovalID: prepared.Decision.ID, ExpectedEpoch: 0, Command: contracts.CommandContext{Scope: contracts.AccessScope{SpaceID: "space", SubjectID: "actor", KnowledgeBaseIDs: []contracts.ID{"kb"}, PolicyVersion: 1}, Principal: contracts.Principal{SubjectID: "actor", SubjectType: "user", Issuer: "fixture"}, Request: contracts.RequestContext{ProfileDigest: contracts.Digest(strings.Repeat("c", 64))}}}
	doc := contracts.Document{ID: f.DocumentID, SpaceID: "space", KnowledgeBaseID: "kb", RowVersion: 1}
	backend := document.PublicationBackendSnapshot{EpochID: "backend", Number: 1, CutoverWatermark: "backend-watermark", IndexProfile: contracts.ProfileReference{ID: "index-profile", Version: 1}, ProfileDigest: contracts.Digest(strings.Repeat("d", 64)), RequiredChannels: []contracts.Channel{contracts.KeywordChannel}}
	return r, doc, view, policy, backend
}

func TestPublicationStartFreezesIndependentPlanWithoutPublication(t *testing.T) {
	r, doc, view, policy, backend := publicationFixture(t)
	plan, err := document.PreparePublicationStart(r, doc, view, policy, 2, backend, 3, "operation", "job", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	backend.RequiredChannels[0] = contracts.VectorChannel
	if plan.Backend.RequiredChannels[0] != contracts.KeywordChannel || plan.Result.Operation.State != "preparing" || plan.Result.Operation.GenerationSetID != nil || plan.Result.Job.State != "queued" || plan.Result.Job.OperationID != "operation" || plan.Result.Job.ProfileDigest != backend.ProfileDigest || doc.PublicationEpoch != 0 || doc.ActiveRevisionID != nil || plan.Review.Validity.ReviewPolicyVersion == plan.Review.Approval.PolicyVersion {
		t.Fatal("start mutated publication or lost independent rule binding")
	}
}
func TestCurrentApprovalRejectsHistoricalHeadAndUnacceptedPolicy(t *testing.T) {
	q, f, at := reviewFixture(t)
	p, err := document.PrepareReview(q, f, 0, "approve", "reviewer", "approve", "", 7, at)
	if err != nil {
		t.Fatal(err)
	}
	policy := document.ReviewPolicySnapshot{Version: 8, AcceptedDecisionPolicyVersions: []uint64{8}}
	policy.Digest, _ = document.ReviewPolicyDigest(policy)
	if _, err := document.CurrentApproval(f, p.Request, p.Decision.ID, p.Decision, p.Decision.ID, policy); err == nil {
		t.Fatal("old rule accepted by current policy")
	}
	policy.AcceptedDecisionPolicyVersions = []uint64{7}
	policy.Digest, _ = document.ReviewPolicyDigest(policy)
	if _, err := document.CurrentApproval(f, p.Request, "revoke", p.Decision, p.Decision.ID, policy); err == nil {
		t.Fatal("historical receipt used as current approval")
	}
}
func TestPublicationStartRejectsEpochAndForgedEffectiveView(t *testing.T) {
	for _, change := range []func(*contracts.PublishRequest, *contracts.Document, *contracts.CurrentPublicationReview, *document.PublicationBackendSnapshot){
		func(r *contracts.PublishRequest, _ *contracts.Document, _ *contracts.CurrentPublicationReview, _ *document.PublicationBackendSnapshot) {
			r.ExpectedEpoch = math.MaxUint64
		},
		func(_ *contracts.PublishRequest, d *contracts.Document, _ *contracts.CurrentPublicationReview, _ *document.PublicationBackendSnapshot) {
			d.PublicationEpoch = 1
		},
		func(_ *contracts.PublishRequest, _ *contracts.Document, v *contracts.CurrentPublicationReview, _ *document.PublicationBackendSnapshot) {
			v.Approval.Decision = "reject"
		},
		func(_ *contracts.PublishRequest, _ *contracts.Document, _ *contracts.CurrentPublicationReview, b *document.PublicationBackendSnapshot) {
			b.RequiredChannels = append(b.RequiredChannels, b.RequiredChannels[0])
		},
		func(_ *contracts.PublishRequest, _ *contracts.Document, _ *contracts.CurrentPublicationReview, b *document.PublicationBackendSnapshot) {
			b.Number = 0
		},
		func(_ *contracts.PublishRequest, _ *contracts.Document, _ *contracts.CurrentPublicationReview, b *document.PublicationBackendSnapshot) {
			b.CutoverWatermark = ""
		},
	} {
		r, d, v, p, b := publicationFixture(t)
		change(&r, &d, &v, &b)
		if _, err := document.PreparePublicationStart(r, d, v, p, 1, b, 1, "op", "job", time.Now()); err == nil {
			t.Fatal("invalid publication start accepted")
		}
	}
}
