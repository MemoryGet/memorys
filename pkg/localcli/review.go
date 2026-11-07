package localcli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

// submitDraft records a review request without approving or publishing it.
// The existing one-click publish path reuses the same idempotent submission
// key, so a user can safely switch between the two workflows.
func (s *state) submitDraft(ctx context.Context, db *sql.DB, owner *authority, id contracts.ID, expected contracts.Digest) (contracts.SubmitResult, error) {
	var empty contracts.SubmitResult
	if owner.principal.SubjectType != "user" || owner.principal.SubjectID != s.m.Owner {
		return empty, errAuthority
	}
	draft, err := s.loadDraft(ctx, db, id)
	if err != nil {
		return empty, err
	}
	frozen, err := document.BuildFrozenContent(draft)
	if err != nil || expected == "" || frozen.ContentHash != expected {
		return empty, errors.New("expected hash does not match the stored draft")
	}
	doc := draft.DocumentID
	command, err := owner.command(ctx, "document.submit", doc, "local-"+digest([]byte(string(s.m.Installation)+"/"+string(id)+"/submit")))
	if err != nil {
		return empty, errAuthority
	}
	submissions := postgres.Submissions{DB: db, Verify: owner.verify, ProfileDigest: s.m.ProfileDigest, ReviewPolicyVersion: 1,
		AuthorizeSubmission: func(c context.Context, q document.SubmissionCommand) error {
			if q.DocumentID != doc || q.Request.DraftID != draft.ID || q.Request.ExpectedVersion != 1 {
				return errAuthority
			}
			return owner.checkCommand(c, q.Request.Command, "document.submit", doc)
		}, VerifyContent: func(c context.Context, scope contracts.AccessScope, d contracts.Draft) error {
			if owner.verify(c, scope) != nil || !reflect.DeepEqual(d, draft) {
				return errAuthority
			}
			if len(d.Blobs) > 0 {
				return s.verifyOriginalDraft(c, owner, d)
			}
			return nil
		}}
	result, err := submissions.SubmitDraft(ctx, document.SubmissionCommand{DocumentID: doc, Request: contracts.SubmitRequest{Command: command, DraftID: id, ExpectedVersion: 1}})
	if err != nil {
		return empty, errors.New("提交审核失败，请检查当前版本后重试")
	}
	if result.Revision.ContentHash != expected {
		return empty, errors.New("提交后的内容摘要不一致")
	}
	return result, nil
}

func (s *state) reviewDraft(ctx context.Context, db *sql.DB, owner *authority, id contracts.ID, decision, reason string) (contracts.Approval, error) {
	var empty contracts.Approval
	if owner.principal.SubjectType != "user" || owner.principal.SubjectID != s.m.Owner {
		return empty, errAuthority
	}
	draft, err := s.loadDraft(ctx, db, id)
	if err != nil {
		return empty, err
	}
	var revision, approvalID string
	if err = db.QueryRowContext(ctx, `SELECT revision_id,approval_id FROM akos_withdraw_v1.submission_receipts WHERE space_id=$1 AND kb_id=$2 AND draft_id=$3`, s.m.Space, s.m.KB, id).Scan(&revision, &approvalID); err != nil {
		return empty, errors.New("请先提交审核这份草稿")
	}
	if decision != "approve" && decision != "reject" {
		return empty, errors.New("审核决定只能是 approve 或 reject")
	}
	if reason == "" {
		reason = map[string]string{"approve": "本地所有者批准审核", "reject": "本地所有者拒绝审核"}[decision]
	}
	doc := draft.DocumentID
	command, err := owner.command(ctx, "revision.approve", doc, "local-"+digest([]byte(string(s.m.Installation)+"/"+string(id)+"/review/"+decision+"/"+reason)))
	if err != nil {
		return empty, errAuthority
	}
	reviews := postgres.Reviews{DB: db, Verify: owner.verify, ProfileDigest: s.m.ProfileDigest, ReviewPolicyVersion: 1, AuthorizeDecision: func(c context.Context, q document.ReviewCommand) error {
		if q.DocumentID != doc || q.Request.RevisionID != contracts.ID(revision) || q.Request.Decision != decision || q.Request.ExpectedApprovalVersion != 0 || q.Request.Reason != reason {
			return errAuthority
		}
		return owner.checkCommand(c, q.Request.Command, "revision.approve", doc)
	}}
	return reviews.DecideReview(ctx, document.ReviewCommand{DocumentID: doc, Request: contracts.ApprovalDecisionRequest{Command: command, RevisionID: contracts.ID(revision), Decision: decision, Reason: reason, ExpectedApprovalVersion: 0}})
}

func (s *state) reviewState(ctx context.Context, db *sql.DB, id contracts.ID) (string, error) {
	state, _, err := s.reviewDetails(ctx, db, id)
	return state, err
}

func (s *state) reviewDetails(ctx context.Context, db *sql.DB, id contracts.ID) (string, *contracts.Approval, error) {
	var state string
	var payload []byte
	err := db.QueryRowContext(ctx, `SELECT a.state,d.payload FROM akos_withdraw_v1.approval_requests a JOIN akos_withdraw_v1.submission_receipts r ON r.space_id=a.space_id AND r.kb_id=a.kb_id AND r.revision_id=a.revision_id LEFT JOIN akos_withdraw_v1.review_decisions d ON d.space_id=a.space_id AND d.kb_id=a.kb_id AND d.id=a.current_decision_id WHERE r.space_id=$1 AND r.kb_id=$2 AND r.draft_id=$3`, s.m.Space, s.m.KB, id).Scan(&state, &payload)
	if err == sql.ErrNoRows {
		return "draft", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if len(payload) == 0 {
		return state, nil, nil
	}
	var approval contracts.Approval
	if len(payload) > 65536 || json.Unmarshal(payload, &approval) != nil || approval.ID == "" || approval.Decision == "" || approval.DecidedAt.Before(time.Unix(0, 0)) {
		return "", nil, errors.New("审核记录无效")
	}
	return state, &approval, nil
}

func (s *state) reviewHistory(ctx context.Context, db *sql.DB, id contracts.ID) ([]contracts.Approval, error) {
	rows, err := db.QueryContext(ctx, `SELECT d.payload FROM akos_withdraw_v1.review_decisions d
JOIN akos_withdraw_v1.submission_receipts r ON r.space_id=d.space_id AND r.kb_id=d.kb_id AND r.revision_id=d.revision_id
WHERE r.space_id=$1 AND r.kb_id=$2 AND r.draft_id=$3 ORDER BY d.row_version LIMIT 32`, s.m.Space, s.m.KB, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []contracts.Approval{}
	for rows.Next() {
		var raw []byte
		var item contracts.Approval
		if rows.Scan(&raw) != nil || len(raw) == 0 || len(raw) > 65536 || json.Unmarshal(raw, &item) != nil || item.ID == "" || item.Decision == "" {
			return nil, errors.New("审核历史无效")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
