package semantic

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type embedFunc func(context.Context, contracts.EmbedRequest) (contracts.EmbedResult, error)

func (f embedFunc) Embed(c context.Context, q contracts.EmbedRequest) (contracts.EmbedResult, error) {
	return f(c, q)
}
func TestSemanticRankingUsesVectorsAndRejectsDimension(t *testing.T) {
	spec := contracts.EmbeddingSpec{Dimension: 16, Distance: "cosine", Normalization: "l2"}
	a, b := make(contracts.Vector, 16), make(contracts.Vector, 16)
	a[0] = 1
	b[1] = 1
	items := []contracts.Candidate{{ChunkID: "a", Text: "budget", Citation: contracts.Citation{DocumentID: "a"}}, {ChunkID: "b", Text: "contacts", Citation: contracts.Citation{DocumentID: "b"}}}
	embed := embedFunc(func(_ context.Context, q contracts.EmbedRequest) (contracts.EmbedResult, error) {
		return contracts.EmbedResult{Spec: q.Spec, Vectors: []contracts.Vector{b, a, b}}, nil
	})
	request := contracts.RequestContext{Deadline: time.Now().Add(time.Second)}
	ranked, cache, err := Rank(context.Background(), request, "who handles outages", items, spec, embed, nil, 10)
	if err != nil || ranked[0].ChunkID != "b" || ranked[0].Score != 1 || len(cache) != 2 {
		t.Fatal("vector ranking differs", err)
	}
	bad := embedFunc(func(_ context.Context, q contracts.EmbedRequest) (contracts.EmbedResult, error) {
		return contracts.EmbedResult{Spec: q.Spec, Vectors: []contracts.Vector{{1}, a, b}}, nil
	})
	if _, _, err = Rank(context.Background(), request, "question", items, spec, bad, nil, 10); err == nil {
		t.Fatal("dimension mismatch accepted")
	}
	if _, _, err = Rank(context.Background(), request, strings.Repeat("q", 4097), items, spec, embed, nil, 10); err == nil {
		t.Fatal("oversize query accepted")
	}
}
