package document

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrPublicationStartRejected = errors.New("publication start rejected without effects from this invocation")
var ErrPublicationStartUnknown = errors.New("publication start commit requires same-key reconciliation")

type ReviewPolicySnapshot struct {
	Version                        uint64
	AcceptedDecisionPolicyVersions []uint64
	Digest                         contracts.Digest
}

// ReviewPolicyDigest validates this default allow-list rule implementation.
// More complex industry rules need their own versioned evaluation provider.
func ReviewPolicyDigest(policy ReviewPolicySnapshot) (contracts.Digest, error) {
	if policy.Version == 0 || len(policy.AcceptedDecisionPolicyVersions) == 0 || len(policy.AcceptedDecisionPolicyVersions) > 64 {
		return "", ErrPublicationStartRejected
	}
	var last uint64
	for _, version := range policy.AcceptedDecisionPolicyVersions {
		if version <= last {
			return "", ErrPublicationStartRejected
		}
		last = version
	}
	raw, err := json.Marshal(map[string]any{"format": "akos.review-policy/1", "version": policy.Version, "accepted_decision_policy_versions": policy.AcceptedDecisionPolicyVersions})
	if err != nil {
		return "", ErrPublicationStartRejected
	}
	encoded, err := canonical.JSON(raw)
	if err != nil {
		return "", ErrPublicationStartRejected
	}
	sum := sha256.Sum256(append([]byte("akos.review-policy/1\x00"), encoded...))
	return contracts.Digest(hex.EncodeToString(sum[:])), nil
}

// CurrentApproval evaluates supplied authoritative facts, never an old receipt
// in isolation. Equal policy numbers are not assumed to imply validity.
func CurrentApproval(fact contracts.FrozenRevisionFact, request contracts.ApprovalRequest, head contracts.ID, approval contracts.Approval, requested contracts.ID, policy ReviewPolicySnapshot) (contracts.CurrentPublicationReview, error) {
	digest, err := ReviewPolicyDigest(policy)
	if err != nil || policy.Digest != digest || !frozenID(fact.ID) || !frozenID(fact.DocumentID) || !frozenID(request.ID) || !frozenID(request.SubmittedBy) || request.SubmittedAt.IsZero() || fact.DocumentID != request.DocumentID || fact.ID != request.RevisionID || fact.HashFormat != "akos.freeze/1" || !candidateDigest(fact.BlocksHash) || !candidateDigest(fact.ContentHash) || request.ContentHash != fact.ContentHash || request.State != "approved" || request.RowVersion == 0 || approval.RowVersion != request.RowVersion || requested != head || head != approval.ID || !frozenID(head) || approval.RevisionID != fact.ID || approval.ContentHash != fact.ContentHash || approval.Decision != "approve" || approval.PolicyVersion == 0 || request.PolicyVersion != approval.PolicyVersion || approval.DecidedAt.IsZero() || !frozenID(approval.ActorID) {
		return contracts.CurrentPublicationReview{}, ErrPublicationStartRejected
	}
	accepted := false
	for _, version := range policy.AcceptedDecisionPolicyVersions {
		accepted = accepted || version == approval.PolicyVersion
	}
	if !accepted {
		return contracts.CurrentPublicationReview{}, ErrPublicationStartRejected
	}
	return contracts.CurrentPublicationReview{Revision: fact, Approval: approval, Validity: contracts.ApprovalValidityFact{ApprovalID: approval.ID, ApprovalRowVersion: approval.RowVersion, ReviewPolicyVersion: policy.Version, State: "effective"}}, nil
}

type PublicationBackendSnapshot struct {
	EpochID          contracts.ID
	Number           uint64
	CutoverWatermark string
	IndexProfile     contracts.ProfileReference
	ProfileDigest    contracts.Digest
	RequiredChannels []contracts.Channel
}
type PublicationStartResult struct {
	Operation contracts.PublicationOperation
	Job       contracts.Job
}
type PublicationStartPlan struct {
	Result                                      PublicationStartResult
	SpaceID, KnowledgeBaseID                    contracts.ID
	OriginalPrincipal                           contracts.Principal
	ProfileDigest                               contracts.Digest
	Review                                      contracts.CurrentPublicationReview
	ReviewPolicyDigest                          contracts.Digest
	ReviewPolicyHeadVersion, BackendHeadVersion uint64
	Backend                                     PublicationBackendSnapshot
	CreatedAt                                   time.Time
}
type PublicationStarter interface {
	StartPublication(context.Context, contracts.PublishRequest) (PublicationStartResult, error)
}

func PreparePublicationStart(request contracts.PublishRequest, doc contracts.Document, review contracts.CurrentPublicationReview, policy ReviewPolicySnapshot, policyHeadVersion uint64, backend PublicationBackendSnapshot, backendHeadVersion uint64, operationID, jobID contracts.ID, at time.Time) (PublicationStartPlan, error) {
	c := request.Command
	if !frozenID(operationID) || !frozenID(jobID) || at.IsZero() || doc.ID != request.DocumentID || doc.SpaceID != c.Scope.SpaceID || len(c.Scope.KnowledgeBaseIDs) != 1 || doc.KnowledgeBaseID != c.Scope.KnowledgeBaseIDs[0] || doc.DeletedAt != nil || doc.RowVersion == 0 || doc.PublicationEpoch != request.ExpectedEpoch || request.ExpectedEpoch == math.MaxUint64 || (doc.ActiveRevisionID == nil) != (doc.ActiveGenerationSetID == nil) || review.Revision.DocumentID != doc.ID || review.Revision.ID != request.RevisionID || review.Approval.ID != request.ApprovalID || review.Validity.ApprovalID != request.ApprovalID || review.Validity.ApprovalRowVersion != review.Approval.RowVersion || review.Validity.State != "effective" || review.Validity.ReviewPolicyVersion != policy.Version || policyHeadVersion == 0 || backendHeadVersion == 0 || !frozenID(backend.EpochID) || backend.Number == 0 || !frozenID(contracts.ID(backend.CutoverWatermark)) || !frozenID(backend.IndexProfile.ID) || backend.IndexProfile.Version == 0 || !candidateDigest(backend.ProfileDigest) || len(backend.RequiredChannels) == 0 || len(backend.RequiredChannels) > 3 || !candidateDigest(c.Request.ProfileDigest) || c.Principal.SubjectID != c.Scope.SubjectID || !frozenID(c.Principal.SubjectID) {
		return PublicationStartPlan{}, ErrPublicationStartRejected
	}
	digest, err := ReviewPolicyDigest(policy)
	if err != nil || digest != policy.Digest {
		return PublicationStartPlan{}, ErrPublicationStartRejected
	}
	accepted := false
	for _, version := range policy.AcceptedDecisionPolicyVersions {
		accepted = accepted || version == review.Approval.PolicyVersion
	}
	if !accepted || review.Approval.Decision != "approve" || review.Approval.ContentHash != review.Revision.ContentHash || !candidateDigest(review.Revision.ContentHash) || !candidateDigest(review.Revision.BlocksHash) || review.Revision.HashFormat != "akos.freeze/1" || review.Approval.RowVersion == 0 || at.UTC().Year() < 1 || at.UTC().Year() > 9999 {
		return PublicationStartPlan{}, ErrPublicationStartRejected
	}
	seen := map[contracts.Channel]bool{}
	for _, channel := range backend.RequiredChannels {
		if (channel != contracts.KeywordChannel && channel != contracts.VectorChannel && channel != contracts.GraphChannel) || seen[channel] {
			return PublicationStartPlan{}, ErrPublicationStartRejected
		}
		seen[channel] = true
	}
	revisionID := request.RevisionID
	result := PublicationStartResult{Operation: contracts.PublicationOperation{ID: operationID, DocumentID: doc.ID, RevisionID: revisionID, ApprovalID: request.ApprovalID, ExpectedEpoch: request.ExpectedEpoch, State: "preparing", ActorID: c.Principal.SubjectID}, Job: contracts.Job{ID: jobID, SpaceID: doc.SpaceID, OperationID: operationID, RevisionID: &revisionID, Stage: "publication.prepare", ProfileDigest: backend.ProfileDigest, State: "queued", RowVersion: 1}}
	actor := c.Principal
	if actor.Delegation != nil {
		d := *actor.Delegation
		d.ProofReference = ""
		actor.Delegation = &d
	}
	plan := PublicationStartPlan{Result: result, SpaceID: doc.SpaceID, KnowledgeBaseID: doc.KnowledgeBaseID, OriginalPrincipal: actor, ProfileDigest: c.Request.ProfileDigest, Review: review, ReviewPolicyDigest: policy.Digest, ReviewPolicyHeadVersion: policyHeadVersion, BackendHeadVersion: backendHeadVersion, Backend: backend, CreatedAt: at.UTC()}
	// Freeze every nested slice/pointer; a provider cannot mutate the plan by
	// changing the caller's backend or principal after this function returns.
	raw, err := json.Marshal(plan)
	if err != nil {
		return PublicationStartPlan{}, ErrPublicationStartRejected
	}
	var owned PublicationStartPlan
	if json.Unmarshal(raw, &owned) != nil {
		return PublicationStartPlan{}, ErrPublicationStartRejected
	}
	return owned, nil
}
