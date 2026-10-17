package queryread_test

import (
	"context"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

func TestHistoryScopeIntersectionKeepsCurrentReadActionsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, mode := range []string{"ok", "read-as-history", "history-as-read", "foreign-subject", "foreign-kb", "policy", "expiry"} {
		s := currentScope()
		s.Action = "knowledge.read"
		h := s
		h.Action = contracts.HistoricalReadAction
		h.KnowledgeBaseIDs = append([]contracts.ID(nil), h.KnowledgeBaseIDs...)
		q := contracts.HistoricalContentReadRequest{Base: contracts.FrozenContentReadRequest{Request: contracts.RequestContext{RequestID: "request", Deadline: time.Now().Add(time.Minute)}, Scope: s, DocumentID: "doc", RevisionID: "old"}, HistoryScope: h, Principal: contracts.Principal{SubjectID: s.SubjectID, SubjectType: "user", Issuer: "fixture", ClientID: "client"}}
		switch mode {
		case "read-as-history":
			q.HistoryScope.Action = "knowledge.read"
		case "history-as-read":
			q.Base.Scope.Action = contracts.HistoricalReadAction
		case "foreign-subject":
			q.HistoryScope.SubjectID = "other"
		case "foreign-kb":
			q.HistoryScope.KnowledgeBaseIDs[0] = "other"
		case "policy":
			q.HistoryScope.PolicyVersion++
		case "expiry":
			q.HistoryScope.ExpiresAt = time.Now().Add(-time.Second)
		}
		err := queryread.ValidateHistoricalScopes(ctx, q)
		if (mode == "ok") != (err == nil) {
			t.Fatal(mode, err)
		}
	}
	if contracts.IsCurrentContentReadAction(contracts.HistoricalReadAction) {
		t.Fatal("history action broadened current reads/search")
	}
}
