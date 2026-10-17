package knowledge

import (
	"reflect"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestRerankingPreservesLineageAndRejectsFabrication(t *testing.T) {
	citation := contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block"}
	input := []contracts.Candidate{{Citation: citation, ChunkID: "first", Text: "中文维护规则", Rank: 1, GenerationIDs: []contracts.ID{"generation"}}, {Citation: citation, ChunkID: "second", Text: "设备故障处理", Rank: 2}}
	ranked := []contracts.Evidence{{Citation: citation, SourceExcerpt: "故障处理", Rank: 1}, {Citation: citation, SourceExcerpt: "维护", Rank: 2}}
	got, err := selectReranked(input, ranked, 2)
	if err != nil || got[0].ChunkID != "second" || got[1].ChunkID != "first" || !reflect.DeepEqual(got[1].GenerationIDs, input[0].GenerationIDs) || got[0].Text != "故障处理" || input[1].Rank != 2 {
		t.Fatal("rerank changed source lineage", err)
	}
	for _, bad := range [][]contracts.Evidence{{{Citation: citation, SourceExcerpt: "模型编造", Rank: 1}}, {{Citation: contracts.Citation{DocumentID: "other"}, SourceExcerpt: "维护", Rank: 1}}, {{Citation: citation, SourceExcerpt: "", Rank: 1}}, {{Citation: citation, SourceExcerpt: "维护", Rank: 2}}, {{Citation: citation, SourceExcerpt: "维护", Rank: 1}, {Citation: citation, SourceExcerpt: "维护", Rank: 2}}} {
		if _, err := selectReranked(input, bad, 2); err == nil {
			t.Fatal("invalid rerank accepted", bad)
		}
	}
}
