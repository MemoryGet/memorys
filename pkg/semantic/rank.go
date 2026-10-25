// Package semantic ranks already-authorized projections. It grants no access
// and does not claim that an authoritative vector generation has been published.
package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrRanking = errors.New("semantic ranking rejected")

func TextKey(text string) string { s := sha256.Sum256([]byte(text)); return hex.EncodeToString(s[:]) }
func validVector(v contracts.Vector, n int) bool {
	if len(v) != n {
		return false
	}
	norm := float64(0)
	for _, x := range v {
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
		norm += f * f
	}
	return math.Abs(norm-1) < 0.01
}

func Rank(ctx context.Context, request contracts.RequestContext, question string, items []contracts.Candidate, spec contracts.EmbeddingSpec, embedder contracts.Embedder, cached map[string]contracts.Vector, limit int) ([]contracts.Candidate, map[string]contracts.Vector, error) {
	if ctx == nil || ctx.Err() != nil || strings.TrimSpace(question) == "" || !utf8.ValidString(question) || len(question) > 4096 || len(items) > 512 || embedder == nil || limit < 1 || limit > 10 || spec.Dimension < 16 || spec.Dimension > 4096 || spec.Distance != "cosine" || spec.Normalization != "l2" {
		return nil, nil, ErrRanking
	}
	texts := []string{question}
	keys := []string{}
	pending := map[string]bool{}
	vectors := map[string]contracts.Vector{}
	for _, item := range items {
		if item.Text == "" || !utf8.ValidString(item.Text) || len(item.Text) > 8192 {
			return nil, nil, ErrRanking
		}
		key := TextKey(item.Text)
		if v := cached[key]; validVector(v, spec.Dimension) {
			vectors[key] = append(contracts.Vector(nil), v...)
		} else if !pending[key] {
			pending[key] = true
			texts = append(texts, item.Text)
			keys = append(keys, key)
		}
	}
	if len(items) == 0 {
		return []contracts.Candidate{}, vectors, nil
	}
	result, err := embedder.Embed(ctx, contracts.EmbedRequest{Request: request, Texts: texts, Spec: spec})
	if err != nil || result.Spec != spec || len(result.Vectors) != len(texts) {
		return nil, nil, ErrRanking
	}
	for _, v := range result.Vectors {
		if !validVector(v, spec.Dimension) {
			return nil, nil, ErrRanking
		}
	}
	for i, key := range keys {
		vectors[key] = append(contracts.Vector(nil), result.Vectors[i+1]...)
	}
	query := result.Vectors[0]
	ranked := make([]contracts.Candidate, 0, len(items))
	for _, item := range items {
		v := vectors[TextKey(item.Text)]
		score := float64(0)
		for i, x := range query {
			score += float64(x) * float64(v[i])
		}
		if score > 1 {
			score = 1
		}
		if score < -1 {
			score = -1
		}
		item.Score = score
		item.ScoreKind = "cosine"
		item.RouteID = "local-semantic-scan"
		item.RetrieverID = "local-ollama-embedding"
		ranked = append(ranked, item)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		if ranked[i].Citation.DocumentID != ranked[j].Citation.DocumentID {
			return ranked[i].Citation.DocumentID < ranked[j].Citation.DocumentID
		}
		return ranked[i].ChunkID < ranked[j].ChunkID
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	for i := range ranked {
		ranked[i].Rank = i + 1
	}
	if ctx.Err() != nil {
		return nil, nil, ErrRanking
	}
	return ranked, vectors, nil
}
