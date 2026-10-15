package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

// Locks use the same document -> review -> policy -> backend order as start.
// A changed head supersedes this immutable plan, even if a newer rule happens
// to accept the same old approval. Rebinding needs a new explicit operation.
func lockPublicationTaskAuthority(ctx context.Context, tx *sql.Tx, plan document.PublicationStartPlan) error {
	args := []any{string(plan.SpaceID), string(plan.KnowledgeBaseID), string(plan.Result.Operation.DocumentID)}
	var epoch string
	var deleted bool
	if err := tx.QueryRowContext(ctx, `SELECT publication_epoch::text,deleted FROM akos_withdraw_v1.documents WHERE space_id=$1::text AND kb_id=$2::text AND id=$3::text FOR SHARE`, args...).Scan(&epoch, &deleted); err != nil {
		if err == sql.ErrNoRows {
			return document.ErrPublicationSuperseded
		}
		return ErrRepository
	}
	if deleted || epoch != strconv.FormatUint(plan.Result.Operation.ExpectedEpoch, 10) {
		return document.ErrPublicationSuperseded
	}
	var payload []byte
	var current sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT payload,current_decision_id FROM akos_withdraw_v1.approval_requests WHERE space_id=$1::text AND kb_id=$2::text AND document_id=$3::text AND revision_id=$4::text FOR SHARE`, append(args, string(plan.Result.Operation.RevisionID))...).Scan(&payload, &current); err != nil {
		if err == sql.ErrNoRows {
			return document.ErrPublicationSuperseded
		}
		return ErrRepository
	}
	var request contracts.ApprovalRequest
	if json.Unmarshal(payload, &request) != nil {
		return ErrRepository
	}
	var version, head, digest string
	var policyBytes []byte
	if err := tx.QueryRowContext(ctx, `SELECT h.policy_version::text,h.row_version::text,p.digest,p.payload FROM akos_withdraw_v1.review_policy_heads h JOIN akos_withdraw_v1.review_policy_snapshots p ON p.space_id=h.space_id AND p.kb_id=h.kb_id AND p.version=h.policy_version WHERE h.space_id=$1::text AND h.kb_id=$2::text FOR SHARE OF h`, args[:2]...).Scan(&version, &head, &digest, &policyBytes); err != nil {
		if err == sql.ErrNoRows {
			return document.ErrPublicationSuperseded
		}
		return ErrRepository
	}
	var policy document.ReviewPolicySnapshot
	if json.Unmarshal(policyBytes, &policy) != nil || strconv.FormatUint(policy.Version, 10) != version || string(policy.Digest) != digest {
		return ErrRepository
	}
	if head != strconv.FormatUint(plan.ReviewPolicyHeadVersion, 10) || policy.Version != plan.Review.Validity.ReviewPolicyVersion || policy.Digest != plan.ReviewPolicyDigest || !current.Valid || current.String != string(plan.Result.Operation.ApprovalID) {
		return document.ErrPublicationSuperseded
	}
	if _, err := document.CurrentApproval(plan.Review.Revision, request, contracts.ID(current.String), plan.Review.Approval, plan.Result.Operation.ApprovalID, policy); err != nil {
		return document.ErrPublicationSuperseded
	}
	var backend, backendVersion string
	if err := tx.QueryRowContext(ctx, `SELECT backend_epoch_id,row_version::text FROM akos_withdraw_v1.publication_backend_heads WHERE space_id=$1::text AND kb_id=$2::text FOR SHARE`, args[:2]...).Scan(&backend, &backendVersion); err != nil {
		if err == sql.ErrNoRows {
			return document.ErrPublicationSuperseded
		}
		return ErrRepository
	}
	if backend != string(plan.Backend.EpochID) || backendVersion != strconv.FormatUint(plan.BackendHeadVersion, 10) {
		return document.ErrPublicationSuperseded
	}
	return nil
}
