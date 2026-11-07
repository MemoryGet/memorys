package localcli

import (
	"context"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestLocalLexicalRerankerPreservesCitationAndLineage(t *testing.T) {
	ranked, err := (localLexicalReranker{}).Rerank(context.Background(), contracts.RerankRequest{Query: "数据库 恢复", Limit: 2, Candidates: []contracts.Candidate{
		{Citation: contracts.Citation{DocumentID: "first", RevisionID: "r", BlockID: "b"}, Score: 0.9, Text: "普通说明"},
		{Citation: contracts.Citation{DocumentID: "second", RevisionID: "r", BlockID: "b"}, Score: 0.2, Text: "数据库故障恢复步骤"},
	}})
	if err != nil || len(ranked) != 2 || ranked[0].Citation.DocumentID != "second" || ranked[0].Rank != 1 || ranked[0].SourceExcerpt != "数据库故障恢复步骤" {
		t.Fatalf("unexpected rerank: %v %#v", err, ranked)
	}
}

func TestLexicalTokensSplitChineseAndWords(t *testing.T) {
	tokens := lexicalTokens("数据库 recovery")
	for _, token := range []string{"数", "据", "库", "recovery"} {
		if _, ok := tokens[token]; !ok {
			t.Fatalf("missing token %q: %#v", token, tokens)
		}
	}
}
