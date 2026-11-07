package localcli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
)

type webDiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// lineDiff uses a bounded LCS for useful small/medium documents and falls
// back to a single changed block for unusually fragmented input.
func lineDiff(left, right []string) []webDiffLine {
	if len(left) == 0 && len(right) == 0 {
		return []webDiffLine{}
	}
	if len(left) > 2000 || len(right) > 2000 || len(left)*len(right) > 4000000 {
		out := make([]webDiffLine, 0, len(left)+len(right))
		for _, line := range left {
			out = append(out, webDiffLine{Kind: "remove", Text: line})
		}
		for _, line := range right {
			out = append(out, webDiffLine{Kind: "add", Text: line})
		}
		return out
	}
	dp := make([][]uint16, len(left)+1)
	for i := range dp {
		dp[i] = make([]uint16, len(right)+1)
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	out := make([]webDiffLine, 0, len(left)+len(right))
	for i, j := 0, 0; i < len(left) || j < len(right); {
		if i < len(left) && j < len(right) && left[i] == right[j] {
			out = append(out, webDiffLine{Kind: "same", Text: left[i]})
			i++
			j++
			continue
		}
		if i < len(left) && (j == len(right) || dp[i+1][j] >= dp[i][j+1]) {
			out = append(out, webDiffLine{Kind: "remove", Text: left[i]})
			i++
		} else {
			out = append(out, webDiffLine{Kind: "add", Text: right[j]})
			j++
		}
	}
	return out
}

func (h *webHost) compareDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DraftID    contracts.ID `json:"draft_id"`
		RevisionID contracts.ID `json:"revision_id"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if q.DraftID == "" || q.RevisionID == "" {
		return errors.New("请提供草稿和要比较的版本。")
	}
	if a == nil || h.s == nil || h.db == nil {
		return errAuthority
	}
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return errAuthority
	}
	draft, err := h.s.loadDraft(ctx, h.db, q.DraftID)
	if err != nil || len(draft.Blocks) != 1 || draft.Blocks[0].Content.Kind != contracts.TextBlock {
		return errors.New("当前草稿不支持版本比较。")
	}
	var raw []byte
	if err = h.db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.revision_contents WHERE space_id=$1 AND kb_id=$2 AND document_id=$3 AND revision_id=$4`, h.s.m.Space, h.s.m.KB, draft.DocumentID, q.RevisionID).Scan(&raw); err != nil || len(raw) > 4<<20 {
		return errors.New("历史版本不存在或不可读取。")
	}
	var revision contracts.Revision
	if json.Unmarshal(raw, &revision) != nil || revision.ID != q.RevisionID || revision.DocumentID != draft.DocumentID || len(revision.Blocks) != 1 || revision.Blocks[0].Content.Kind != contracts.TextBlock {
		return errors.New("历史版本内容无效。")
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	left, right := draft.Blocks[0].Content.Text, revision.Blocks[0].Content.Text
	lines := lineDiff(strings.Split(left, "\n"), strings.Split(right, "\n"))
	if len(lines) > 10000 {
		return errors.New("版本差异过大，请缩小文档后再比较。")
	}
	frozenLeft, err := document.BuildFrozenContent(draft)
	if err != nil {
		return err
	}
	return webJSON(w, map[string]any{"draft_id": draft.ID, "document_id": draft.DocumentID, "draft_hash": frozenLeft.ContentHash, "revision_id": revision.ID, "revision_hash": revision.ContentHash, "lines": lines})
}
