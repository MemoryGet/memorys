package knowledge

import (
	"context"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestLexicalRerankerPreservesCitationAndRanksOverlap(t *testing.T) {
	ranked, err := (LexicalReranker{}).Rerank(context.Background(), contracts.RerankRequest{Query: "数据库 恢复", Limit: 2, Candidates: []contracts.Candidate{
		{Text: "普通说明", Score: 1, Citation: contracts.Citation{DocumentID: "d1", RevisionID: "r1", BlockID: "b1"}},
		{Text: "数据库故障恢复步骤", Score: 0.1, Citation: contracts.Citation{DocumentID: "d2", RevisionID: "r2", BlockID: "b2"}},
	}})
	if err != nil || len(ranked) != 2 || ranked[0].Citation.DocumentID != "d2" || ranked[0].Rank != 1 || ranked[0].SourceExcerpt != "数据库故障恢复步骤" {
		t.Fatalf("ranked=%#v err=%v", ranked, err)
	}
}
