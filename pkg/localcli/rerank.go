package localcli

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// localLexicalReranker is a bounded, dependency-free second pass for the
// local hybrid route. It only reorders supplied candidates and never changes
// excerpts or citation identity; a model reranker can replace it later behind
// the same contracts.Reranker port.
type localLexicalReranker struct{}

func (localLexicalReranker) Rerank(ctx context.Context, request contracts.RerankRequest) ([]contracts.Evidence, error) {
	if ctx == nil || request.Query == "" || request.Limit < 1 || request.Limit > len(request.Candidates) || len(request.Candidates) > 100 {
		return nil, errState
	}
	queryTokens := lexicalTokens(request.Query)
	if len(queryTokens) == 0 {
		return nil, errState
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
		for token := range queryTokens {
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
	for index := 0; index < request.Limit; index++ {
		candidate := items[index].candidate
		result = append(result, contracts.Evidence{Citation: candidate.Citation, SourceExcerpt: candidate.Text, Rank: index + 1})
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
