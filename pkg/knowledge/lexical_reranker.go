package knowledge

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// LexicalReranker is the dependency-free default reranker. It only reorders
// already authorized candidates; it cannot add evidence or alter citations.
// Hosts can replace it with a model-backed contracts.Reranker implementation.
type LexicalReranker struct{}

func (LexicalReranker) Rerank(ctx context.Context, request contracts.RerankRequest) ([]contracts.Evidence, error) {
	if ctx == nil || request.Query == "" || request.Limit < 1 || request.Limit > len(request.Candidates) || len(request.Candidates) > 1000 {
		return nil, ErrSearch
	}
	tokens := lexicalTokens(request.Query)
	if len(tokens) == 0 {
		return nil, ErrSearch
	}
	type ranked struct {
		candidate contracts.Candidate
		overlap   int
		index     int
	}
	items := make([]ranked, 0, len(request.Candidates))
	for index, candidate := range request.Candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text := strings.ToLower(candidate.Text)
		overlap := 0
		for token := range tokens {
			if strings.Contains(text, token) {
				overlap++
			}
		}
		items = append(items, ranked{candidate: candidate, overlap: overlap, index: index})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].overlap != items[j].overlap {
			return items[i].overlap > items[j].overlap
		}
		if items[i].candidate.Score != items[j].candidate.Score {
			return items[i].candidate.Score > items[j].candidate.Score
		}
		return items[i].index < items[j].index
	})
	result := make([]contracts.Evidence, 0, request.Limit)
	for i := 0; i < request.Limit; i++ {
		candidate := items[i].candidate
		result = append(result, contracts.Evidence{Citation: candidate.Citation, SourceExcerpt: candidate.Text, Rank: i + 1})
	}
	return result, nil
}

func lexicalTokens(value string) map[string]struct{} {
	tokens := map[string]struct{}{}
	var word []rune
	flush := func() {
		if len(word) > 0 {
			tokens[strings.ToLower(string(word))] = struct{}{}
			word = nil
		}
	}
	for _, r := range []rune(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if unicode.Is(unicode.Han, r) {
				flush()
				tokens[string(r)] = struct{}{}
			} else {
				word = append(word, r)
			}
			continue
		}
		flush()
	}
	flush()
	return tokens
}
