package document

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
	"math/rand"
	"testing"
)

func TestTableSweepMatchesIndependentRectangleOracle(t *testing.T) {
	random := rand.New(rand.NewSource(219))
	for trial := 0; trial < 1000; trial++ {
		cells := make([]contracts.TableCell, random.Intn(30))
		for i := range cells {
			cells[i] = contracts.TableCell{Row: random.Intn(20), Column: random.Intn(20), RowSpan: 1 + random.Intn(5), ColumnSpan: 1 + random.Intn(5)}
		}
		want := false
		for i, a := range cells {
			for _, b := range cells[i+1:] {
				if a.Row < b.Row+b.RowSpan && b.Row < a.Row+a.RowSpan && a.Column < b.Column+b.ColumnSpan && b.Column < a.Column+a.ColumnSpan {
					want = true
				}
			}
		}
		if actual := overlappingCells(cells); actual != want {
			t.Fatalf("sweep differs at trial %d: %+v", trial, cells)
		}
	}
}

func TestTableSweepAllowsTouchingAndHugeSparseSpans(t *testing.T) {
	cells := []contracts.TableCell{{Row: 0, Column: 0, RowSpan: 1000000000, ColumnSpan: 1}, {Row: 0, Column: 1, RowSpan: 1000000000, ColumnSpan: 1}, {Row: 1000000000, Column: 0, RowSpan: 1, ColumnSpan: 2}}
	if overlappingCells(cells) {
		t.Fatal("adjacent boundaries treated as overlap")
	}
	cells = append(cells, contracts.TableCell{Row: 999999999, Column: 0, RowSpan: 1, ColumnSpan: 1})
	if !overlappingCells(cells) {
		t.Fatal("late overlap missed")
	}
}
