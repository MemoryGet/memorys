package localruntime

import (
	"sort"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// mergeFederatedCandidates performs the final deterministic top-k merge for
// independent KB retrievals. Each partition is already current-authority
// checked by knowledge.SearchService, so this function only owns ordering and
// rank normalization; it never invents or rewrites evidence.
func mergeFederatedCandidates(in []contracts.Candidate, limit int) []contracts.Candidate {
	if limit < 1 || len(in) == 0 {
		return []contracts.Candidate{}
	}
	out := append([]contracts.Candidate(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].KnowledgeBaseID != out[j].KnowledgeBaseID {
			return out[i].KnowledgeBaseID < out[j].KnowledgeBaseID
		}
		if out[i].ChunkID != out[j].ChunkID {
			return out[i].ChunkID < out[j].ChunkID
		}
		if out[i].RouteID != out[j].RouteID {
			return out[i].RouteID < out[j].RouteID
		}
		return out[i].RetrieverID < out[j].RetrieverID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}
