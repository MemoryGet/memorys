package localcli

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type webAuditRecord struct {
	Action     string       `json:"action"`
	RequestID  contracts.ID `json:"request_id"`
	ActorID    contracts.ID `json:"actor_id"`
	ResourceID contracts.ID `json:"resource_id"`
	OccurredAt string       `json:"occurred_at"`
}

func (h *webHost) auditList(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return errors.New("审计记录数量范围为 1-100。")
		}
		limit = parsed
	}
	scope, err := a.scope(ctx, "knowledge.search", "")
	if err != nil || a.verify(ctx, scope) != nil {
		return errAuthority
	}
	const query = `SELECT action,request_id,actor_id,resource_id,occurred_at FROM (
 SELECT action::text,request_id::text,actor_id::text,candidate_id::text,occurred_at::text FROM akos_withdraw_v1.candidate_audit WHERE space_id=$1 AND kb_id=$2
 UNION ALL
 SELECT action::text,request_id::text,actor_id::text,revision_id::text,occurred_at::text FROM akos_withdraw_v1.submission_audit WHERE space_id=$1 AND kb_id=$2
 UNION ALL
 SELECT action::text,request_id::text,actor_id::text,decision_id::text,occurred_at::text FROM akos_withdraw_v1.review_audit WHERE space_id=$1 AND kb_id=$2
 UNION ALL
 SELECT action::text,request_id::text,actor_id::text,operation_id::text,occurred_at::text FROM akos_withdraw_v1.publication_start_audit WHERE space_id=$1 AND kb_id=$2
 UNION ALL
 SELECT action::text,request_id::text,actor_id::text,document_id::text,occurred_at::text FROM akos_withdraw_v1.withdraw_audit WHERE space_id=$1 AND kb_id=$2
 UNION ALL
 SELECT action::text,request_id::text,actor_id::text,document_id::text,occurred_at::text FROM akos_withdraw_v1.document_delete_audit WHERE space_id=$1 AND kb_id=$2
) records ORDER BY occurred_at DESC LIMIT $3`
	rows, err := h.db.QueryContext(ctx, query, h.s.m.Space, h.s.m.KB, limit)
	if err != nil {
		return errDatabase
	}
	defer rows.Close()
	items := make([]webAuditRecord, 0, limit)
	for rows.Next() {
		var item webAuditRecord
		if err := rows.Scan(&item.Action, &item.RequestID, &item.ActorID, &item.ResourceID, &item.OccurredAt); err != nil {
			return errDatabase
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return errDatabase
	}
	return webJSON(w, map[string]any{"audit": items, "count": len(items)})
}
