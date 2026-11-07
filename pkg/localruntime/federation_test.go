package localruntime

import (
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestMergeFederatedCandidatesSortsLimitsAndNormalizesRanks(t *testing.T) {
	got := mergeFederatedCandidates([]contracts.Candidate{
		{KnowledgeBaseID: "kb-b", ChunkID: "b", Score: 2, Rank: 9},
		{KnowledgeBaseID: "kb-a", ChunkID: "a2", Score: 3, Rank: 4},
		{KnowledgeBaseID: "kb-a", ChunkID: "a1", Score: 3, Rank: 1},
	}, 2)
	if len(got) != 2 || got[0].KnowledgeBaseID != "kb-a" || got[0].ChunkID != "a1" || got[1].ChunkID != "a2" || got[0].Rank != 1 || got[1].Rank != 2 {
		t.Fatalf("unexpected federated merge: %+v", got)
	}
	if got := mergeFederatedCandidates(nil, 2); len(got) != 0 {
		t.Fatalf("nil merge should be empty, got %+v", got)
	}
}
