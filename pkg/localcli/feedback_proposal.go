package localcli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// createFeedbackProposal turns an actionable source issue into a normal local
// draft. It never edits the published revision and never bypasses review.
func (h *webHost) createFeedbackProposal(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		FeedbackID contracts.ID `json:"feedback_id"`
	}
	if err := readWebJSON(w, r, &q); err != nil || q.FeedbackID == "" {
		return errors.New("请提供反馈标识。")
	}
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	records, err := readFeedback(h.s.root)
	if err != nil {
		return errDatabase
	}
	var feedback *localFeedback
	for i := range records {
		if records[i].ID == q.FeedbackID {
			copy := records[i]
			copy.normalize()
			feedback = &copy
			break
		}
	}
	if feedback == nil || feedback.DocumentID == "" || feedback.RevisionID == "" || feedback.Kind == "helpful" || feedback.Kind == "other" {
		return errors.New("该反馈不能创建修订草稿。")
	}
	var sourceID contracts.ID
	if err := h.db.QueryRowContext(ctx, `SELECT id FROM akos_withdraw_v1.draft_heads WHERE space_id=$1 AND kb_id=$2 AND document_id=$3 ORDER BY row_version DESC,id DESC LIMIT 1`, h.s.m.Space, h.s.m.KB, feedback.DocumentID).Scan(&sourceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("反馈对应的文档草稿不存在。")
		}
		return errDatabase
	}
	draft, err := h.s.loadDraft(ctx, h.db, sourceID)
	if err != nil || len(draft.Blocks) != 1 || draft.Blocks[0].Content.Kind != contracts.TextBlock {
		return errors.New("该文档当前不支持从反馈创建文本草稿。")
	}
	result, err := h.s.editDraft(ctx, h.db, a, sourceID, draft.Blocks[0].Content.Text)
	if err != nil {
		return err
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	records, err = readFeedback(h.s.root)
	if err != nil {
		return errDatabase
	}
	found := false
	for i := range records {
		records[i].normalize()
		if records[i].ID != q.FeedbackID {
			continue
		}
		records[i].ProposalDraftID = result.DraftID
		found = true
		break
	}
	if !found {
		return errors.New("反馈在创建草稿期间已不存在。")
	}
	raw, err := json.Marshal(records)
	if err != nil || len(raw) > 8<<20 || writeAtomic(h.s.root, "feedback.json", raw) != nil {
		return errDatabase
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"feedback_id": q.FeedbackID, "draft_id": result.DraftID, "document_id": result.DocumentID, "content_hash": result.ContentHash, "state": "draft"})
}
