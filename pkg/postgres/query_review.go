package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

// Re-evaluate a publication's original approval under the current rules.
func (r *queryRepository) loadReview(ctx context.Context, space, kb, documentID, revisionID, approvalID contracts.ID, prepared contracts.CurrentPublicationReview) (contracts.CurrentPublicationReview, error) {
	var requestBytes, policyBytes []byte
	var current sql.NullString
	var policyDigest string
	if r.tx.QueryRowContext(ctx, `SELECT payload,current_decision_id FROM akos_withdraw_v1.approval_requests WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND revision_id=$4::text`, string(space), string(kb), string(documentID), string(revisionID)).Scan(&requestBytes, &current) != nil {
		return contracts.CurrentPublicationReview{}, queryread.ErrRead
	}
	if r.tx.QueryRowContext(ctx, `SELECT p.payload,p.digest FROM akos_withdraw_v1.review_policy_heads h JOIN akos_withdraw_v1.review_policy_snapshots p ON p.space_id=h.space_id AND p.kb_id=h.kb_id AND p.version=h.policy_version WHERE h.space_id=$1::text AND h.kb_id=$2::text`, string(space), string(kb)).Scan(&policyBytes, &policyDigest) != nil {
		return contracts.CurrentPublicationReview{}, queryread.ErrRead
	}
	var q contracts.ApprovalRequest
	var policy document.ReviewPolicySnapshot
	if len(requestBytes) > 65536 || len(policyBytes) > 65536 || json.Unmarshal(requestBytes, &q) != nil || json.Unmarshal(policyBytes, &policy) != nil || policy.Digest != contracts.Digest(policyDigest) {
		return contracts.CurrentPublicationReview{}, queryread.ErrRead
	}
	if digest, err := document.ReviewPolicyDigest(policy); err != nil || digest != policy.Digest {
		return contracts.CurrentPublicationReview{}, queryread.ErrRead
	}
	review := prepared
	review.Validity = contracts.ApprovalValidityFact{ApprovalID: review.Approval.ID, ApprovalRowVersion: review.Approval.RowVersion, ReviewPolicyVersion: policy.Version, State: "stale"}
	if current.Valid {
		if actual, err := document.CurrentApproval(review.Revision, q, contracts.ID(current.String), prepared.Approval, approvalID, policy); err == nil {
			review = actual
		}
	}
	return review, nil
}
