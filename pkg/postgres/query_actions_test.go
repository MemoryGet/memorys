package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/publicationprojection"
)

func TestMetadataReadActionsAndSearchBoundary(t *testing.T) {
	for _, action := range []contracts.Action{"knowledge.search", "knowledge.read", "knowledge.read.history", contracts.HistoricalReadAction, "knowledge.*", "knowledge.write", ""} {
		scope := testScope()
		scope.Action = action
		want := action == "knowledge.search" || action == "knowledge.read"
		if validQueryScope(scope) != want {
			t.Fatalf("metadata action %q", action)
		}
	}
	c, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	r := KeywordRetriever{ModuleInstanceID: "keyword", Authorize: func(context.Context, contracts.RetrieveRequest) ([]byte, publicationprojection.CallIdentity, error) {
		calls++
		return nil, publicationprojection.CallIdentity{}, nil
	}}
	_, err := r.Search(c, contracts.RetrieveRequest{Scope: contracts.AccessScope{Action: "knowledge.read"}})
	if err != ErrKeywordSearch || calls != 0 {
		t.Fatalf("read scope reached search authority: %d %v", calls, err)
	}
}
