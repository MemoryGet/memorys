// Package fusion provides a bounded rank-based Fuser. Authorization, publication
// and citation authenticity must be checked by the surrounding application.
package fusion

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"math"
	"math/big"
	"sort"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid fusion input")

type RRF struct{}

var _ contracts.Fuser = RRF{}

type aggregate struct {
	candidate   contracts.Candidate
	weight      *big.Rat
	key         string
	origins     []contracts.FusionOrigin
	generations map[contracts.ID]bool
}

func sourceKey(c contracts.Candidate) (string, error) {
	data, err := json.Marshal(struct {
		KB       contracts.ID
		Chunk    contracts.ID
		Citation contracts.Citation
	}{c.KnowledgeBaseID, c.ChunkID, c.Citation})
	return string(data), err
}
func validID(v contracts.ID) bool { return v != "" && utf8.ValidString(string(v)) }
func ids(values map[contracts.ID]bool) []contracts.ID {
	out := make([]contracts.ID, 0, len(values))
	for id := range values {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (RRF) Fuse(ctx context.Context, request contracts.FuseRequest) ([]contracts.Candidate, error) {
	if ctx == nil || request.Request.Deadline.IsZero() || !time.Now().Before(request.Request.Deadline) || request.RankConstant < 1 || request.RankConstant > 10000 || request.Limit < 1 || request.Limit > 1000 || len(request.Lists) > 32 {
		return nil, ErrInvalid
	}
	work, cancel := context.WithDeadline(ctx, request.Request.Deadline)
	defer cancel()
	routes := map[contracts.ID]contracts.RankedList{}
	total := 0
	textBytes := 0
	for _, list := range request.Lists {
		if err := work.Err(); err != nil {
			return nil, err
		}
		total += len(list.Candidates)
		if total > 10000 || len(list.Candidates) > 1000 || !validID(list.RouteID) {
			return nil, ErrInvalid
		}
		for _, candidate := range list.Candidates {
			textBytes += len(candidate.Text)
			if textBytes > 16<<20 {
				return nil, ErrInvalid
			}
		}
		if prior, exists := routes[list.RouteID]; exists {
			a, e1 := json.Marshal(prior)
			b, e2 := json.Marshal(list)
			if e1 != nil || e2 != nil || string(a) != string(b) {
				return nil, ErrInvalid
			}
			continue // An identical repeated route contributes once.
		}
		routes[list.RouteID] = list
	}
	order := make([]contracts.ID, 0, len(routes))
	for route := range routes {
		order = append(order, route)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	all := map[string]*aggregate{}
	for _, route := range order {
		seen := map[string]bool{}
		previousRank := 0
		for _, c := range routes[route].Candidates {
			if err := work.Err(); err != nil {
				return nil, err
			}
			if c.Rank <= previousRank || c.Rank > 1000 || c.RouteID != route || !validID(c.RetrieverID) || !validID(c.ChunkID) || !validID(c.KnowledgeBaseID) || !validID(c.Citation.DocumentID) || !validID(c.Citation.RevisionID) || !validID(c.Citation.BlockID) || !utf8.ValidString(c.Text) || math.IsNaN(c.Score) || math.IsInf(c.Score, 0) || len(c.GenerationIDs) == 0 || len(c.FusionOrigins) != 0 {
				return nil, ErrInvalid
			}
			previousRank = c.Rank
			gens := map[contracts.ID]bool{}
			for _, g := range c.GenerationIDs {
				if !validID(g) {
					return nil, ErrInvalid
				}
				gens[g] = true
			}
			key, err := sourceKey(c)
			if err != nil {
				return nil, ErrInvalid
			}
			a, exists := all[key]
			if !exists {
				// Clone the nested citation, so outputs do not share mutable source slices.
				raw, err := json.Marshal(c)
				if err != nil {
					return nil, ErrInvalid
				}
				var cloned contracts.Candidate
				if json.Unmarshal(raw, &cloned) != nil {
					return nil, ErrInvalid
				}
				a = &aggregate{candidate: cloned, weight: new(big.Rat), key: key, generations: map[contracts.ID]bool{}}
				all[key] = a
			} else if a.candidate.Text != c.Text {
				return nil, ErrInvalid
			}
			for g := range gens {
				a.generations[g] = true
			}
			if seen[key] { // Duplicate within a route consumes rank space, but no extra vote.
				last := &a.origins[len(a.origins)-1]
				if last.RouteID != route || last.RetrieverID != c.RetrieverID {
					return nil, ErrInvalid
				}
				merged := map[contracts.ID]bool{}
				for _, g := range last.GenerationIDs {
					merged[g] = true
				}
				for g := range gens {
					merged[g] = true
				}
				last.GenerationIDs = ids(merged)
				continue
			}
			seen[key] = true
			a.weight.Add(a.weight, new(big.Rat).SetFrac64(1, int64(request.RankConstant+c.Rank)))
			a.origins = append(a.origins, contracts.FusionOrigin{RouteID: route, RetrieverID: c.RetrieverID, Rank: c.Rank, GenerationIDs: ids(gens)})
		}
	}
	ordered := make([]*aggregate, 0, len(all))
	for _, a := range all {
		ordered = append(ordered, a)
	}
	sort.Slice(ordered, func(i, j int) bool {
		comparison := ordered[i].weight.Cmp(ordered[j].weight)
		if comparison == 0 {
			return ordered[i].key < ordered[j].key
		}
		return comparison > 0
	})
	if len(ordered) > request.Limit {
		ordered = ordered[:request.Limit]
	}
	result := make([]contracts.Candidate, 0, len(ordered))
	for index, a := range ordered {
		c := a.candidate
		c.Rank = index + 1
		c.Score, _ = a.weight.Float64()
		c.ScoreKind = "rrf"
		c.GenerationIDs = ids(a.generations)
		c.FusionOrigins = a.origins
		result = append(result, c)
	}
	if err := work.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
