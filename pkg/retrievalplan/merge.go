package retrievalplan

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type PartitionResult struct {
	Target     Target
	Candidates []contracts.Candidate
}

// PartitionOrigin is internal diagnostic data, not an authenticated citation.
type PartitionOrigin struct {
	Target       Target
	OriginalRank int
	GlobalRank   int
}

type MergeResult struct {
	List    contracts.RankedList
	Origins []PartitionOrigin
	// Serialized raw-candidate cost, for the execution-wide ingress budget.
	EncodedBytes int
}

const MaxCandidateBytes = 16 << 20

// Merge combines successful, already source-validated partition results. Every
// planned target must be present exactly once. Failures are handled upstream.
// Scope/publication and generation membership must still be checked upstream.
func Merge(ctx context.Context, route Route, results []PartitionResult) (MergeResult, error) {
	if ctx == nil || !idValid(route.ID) || route.Limit < 1 || route.Limit > 1000 || len(route.Calls) > 256 || len(results) != len(route.Calls) {
		return MergeResult{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return MergeResult{}, err
	}
	calls := append([]Call(nil), route.Calls...)
	sort.Slice(calls, func(i, j int) bool { return calls[i].Target.KnowledgeBaseID < calls[j].Target.KnowledgeBaseID })
	expected := map[Target]Call{}
	quota := 0
	for i, call := range calls {
		if call.RouteID != route.ID || call.Required != route.Required || !idValid(call.RetrieverID) || !idValid(call.Target.KnowledgeBaseID) || !idValid(call.Target.BackendEpochID) || call.Limit < 1 || call.Limit > route.Limit || (i > 0 && calls[i-1].Target.KnowledgeBaseID == call.Target.KnowledgeBaseID) {
			return MergeResult{}, ErrInvalid
		}
		if i > 0 && call.RetrieverID != calls[0].RetrieverID {
			return MergeResult{}, ErrInvalid
		}
		quota += call.Limit
		if quota > route.Limit {
			return MergeResult{}, ErrInvalid
		}
		expected[call.Target] = call
	}
	if len(calls) > 0 && quota != route.Limit {
		return MergeResult{}, ErrInvalid
	}
	lists := map[Target][]contracts.Candidate{}
	bytes := 0
	for _, result := range results {
		if err := ctx.Err(); err != nil {
			return MergeResult{}, err
		}
		call, ok := expected[result.Target]
		if _, duplicate := lists[result.Target]; !ok || duplicate || len(result.Candidates) > call.Limit {
			return MergeResult{}, ErrInvalid
		}
		previous := 0
		copyList := make([]contracts.Candidate, 0, len(result.Candidates))
		for _, c := range result.Candidates {
			if err := ctx.Err(); err != nil {
				return MergeResult{}, err
			}
			if c.RouteID != route.ID || c.RetrieverID != call.RetrieverID || c.KnowledgeBaseID != call.Target.KnowledgeBaseID || !idValid(c.ChunkID) || !idValid(c.Citation.DocumentID) || !idValid(c.Citation.RevisionID) || !idValid(c.Citation.BlockID) || c.Rank <= previous || c.Rank > 1000 || math.IsNaN(c.Score) || math.IsInf(c.Score, 0) || !utf8.ValidString(c.Text) || c.FusionOrigins != nil || len(c.GenerationIDs) == 0 {
				return MergeResult{}, ErrInvalid
			}
			previous = c.Rank
			for _, g := range c.GenerationIDs {
				if !idValid(g) {
					return MergeResult{}, ErrInvalid
				}
			}
			// Input DTO/source limits must be checked before calling this helper.
			raw, err := json.Marshal(c)
			if err != nil {
				return MergeResult{}, ErrInvalid
			}
			bytes += len(raw)
			if bytes > MaxCandidateBytes {
				return MergeResult{}, ErrInvalid
			}
			var clone contracts.Candidate
			if json.Unmarshal(raw, &clone) != nil {
				return MergeResult{}, ErrInvalid
			}
			copyList = append(copyList, clone)
		}
		lists[result.Target] = copyList
	}
	out := MergeResult{List: contracts.RankedList{RouteID: route.ID}, EncodedBytes: bytes}
	for position := 0; ; position++ {
		added := false
		for _, call := range calls {
			if err := ctx.Err(); err != nil {
				return MergeResult{}, err
			}
			list := lists[call.Target]
			if position >= len(list) {
				continue
			}
			c := list[position]
			rank := len(out.List.Candidates) + 1
			out.Origins = append(out.Origins, PartitionOrigin{Target: call.Target, OriginalRank: c.Rank, GlobalRank: rank})
			c.Rank = rank
			out.List.Candidates = append(out.List.Candidates, c)
			added = true
		}
		if !added {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return MergeResult{}, err
	}
	return out, nil
}
