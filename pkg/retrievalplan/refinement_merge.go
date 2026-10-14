package retrievalplan

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"sort"
	"strings"
	"unicode/utf8"
)

type RefinementResult struct {
	QueryIndex int
	Result     PartitionResult
}
type RefinementOrigin struct {
	QueryIndex               int
	Target                   Target
	OriginalRank, GlobalRank int
}
type RefinementMerge struct {
	List         contracts.RankedList
	Origins      []RefinementOrigin
	EncodedBytes int
}

// MergeRefinement first merges KB partitions, then interleaves query variants
// into ONE route. It neither authenticates results nor handles partial failure.
func MergeRefinement(ctx context.Context, route RefinementRoute, results []RefinementResult) (RefinementMerge, error) {
	if ctx == nil || !idValid(route.ID) || route.Limit < 1 || route.Limit > 1000 || len(route.Calls) > 256 || len(results) != len(route.Calls) {
		return RefinementMerge{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return RefinementMerge{}, err
	}
	groups := map[int]Route{}
	texts := map[int]string{}
	total := 0
	var retriever contracts.ID
	for _, planned := range route.Calls {
		call := planned.Call
		if planned.QueryIndex < 0 || planned.QueryIndex > 3 || !utf8.ValidString(planned.QueryText) || strings.TrimSpace(planned.QueryText) == "" || utf8.RuneCountInString(planned.QueryText) > 16000 || call.RouteID != route.ID || call.Required != route.Required || call.Limit < 1 || call.Limit > route.Limit {
			return RefinementMerge{}, ErrInvalid
		}
		if old, exists := texts[planned.QueryIndex]; exists && old != planned.QueryText {
			return RefinementMerge{}, ErrInvalid
		}
		texts[planned.QueryIndex] = planned.QueryText
		if retriever != "" && retriever != call.RetrieverID {
			return RefinementMerge{}, ErrInvalid
		}
		retriever = call.RetrieverID
		group := groups[planned.QueryIndex]
		group.ID = route.ID
		group.Required = route.Required
		group.Limit += call.Limit
		group.Calls = append(group.Calls, call)
		groups[planned.QueryIndex] = group
		total += call.Limit
		if total > route.Limit {
			return RefinementMerge{}, ErrInvalid
		}
	}
	if len(route.Calls) > 0 && total != route.Limit {
		return RefinementMerge{}, ErrInvalid
	}
	partitioned := map[int][]PartitionResult{}
	for _, result := range results {
		if _, ok := groups[result.QueryIndex]; !ok {
			return RefinementMerge{}, ErrInvalid
		}
		partitioned[result.QueryIndex] = append(partitioned[result.QueryIndex], result.Result)
	}
	var indexes []int
	for index := range groups {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	merged := map[int]MergeResult{}
	out := RefinementMerge{List: contracts.RankedList{RouteID: route.ID}}
	for _, index := range indexes {
		value, err := Merge(ctx, groups[index], partitioned[index])
		if err != nil {
			return RefinementMerge{}, err
		}
		out.EncodedBytes += value.EncodedBytes + 3*len(value.List.Candidates)
		if out.EncodedBytes > MaxCandidateBytes {
			return RefinementMerge{}, ErrInvalid
		}
		merged[index] = value
	}
	for position := 0; ; position++ {
		added := false
		for _, index := range indexes {
			if err := ctx.Err(); err != nil {
				return RefinementMerge{}, err
			}
			value := merged[index]
			if position >= len(value.List.Candidates) {
				continue
			}
			c := value.List.Candidates[position]
			origin := value.Origins[position]
			rank := len(out.List.Candidates) + 1
			out.Origins = append(out.Origins, RefinementOrigin{QueryIndex: index, Target: origin.Target, OriginalRank: origin.OriginalRank, GlobalRank: rank})
			c.Rank = rank
			out.List.Candidates = append(out.List.Candidates, c)
			added = true
		}
		if !added {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return RefinementMerge{}, err
	}
	return out, nil
}
