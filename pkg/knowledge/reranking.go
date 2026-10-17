package knowledge

import (
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// selectReranked permits only reordering/filtering and excerpts of input text.
// It retains the selected candidate's platform lineage and retrieval scores.
func selectReranked(input []contracts.Candidate, ranked []contracts.Evidence, limit int) ([]contracts.Candidate, error) {
	if len(ranked) > limit || len(ranked) > len(input) {
		return nil, ErrSearch
	}
	used := make([]bool, len(input))
	out := make([]contracts.Candidate, 0, len(ranked))
	for i, e := range ranked {
		if e.Rank != i+1 || e.SourceExcerpt == "" || !utf8.ValidString(e.SourceExcerpt) {
			return nil, ErrSearch
		}
		found := -1
		for j, c := range input {
			if !used[j] && reflect.DeepEqual(c.Citation, e.Citation) && strings.Contains(c.Text, e.SourceExcerpt) {
				found = j
				break
			}
		}
		if found < 0 {
			return nil, ErrSearch
		}
		used[found] = true
		c := input[found]
		c.Text = e.SourceExcerpt
		c.Rank = e.Rank
		out = append(out, c)
	}
	return out, nil
}
