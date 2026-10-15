package document

import (
	"github.com/MemoryGet/memorys/pkg/contracts"
	"sort"
)

// Coordinate-compressed sweep: O(n log n), without expanding huge row spans or
// doing quadratic comparisons for sparse/merged tables.
func overlappingCells(cells []contracts.TableCell) bool {
	if len(cells) == 0 {
		return false
	}
	type event struct{ row, left, right, delta int }
	columns := make([]int, 0, len(cells)*2)
	events := make([]event, 0, len(cells)*2)
	for _, c := range cells {
		columns = append(columns, c.Column, c.Column+c.ColumnSpan)
		events = append(events, event{c.Row, c.Column, c.Column + c.ColumnSpan, 1}, event{c.Row + c.RowSpan, c.Column, c.Column + c.ColumnSpan, -1})
	}
	sort.Ints(columns)
	unique := columns[:0]
	for _, v := range columns {
		if len(unique) == 0 || unique[len(unique)-1] != v {
			unique = append(unique, v)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].row == events[j].row {
			return events[i].delta < events[j].delta
		}
		return events[i].row < events[j].row
	})
	maximum, lazy := make([]int, 4*len(unique)), make([]int, 4*len(unique))
	var add func(int, int, int, int, int, int)
	add = func(node, left, right, first, last, delta int) {
		if first <= left && right <= last {
			maximum[node] += delta
			lazy[node] += delta
			return
		}
		middle := (left + right) / 2
		if first <= middle {
			add(node*2, left, middle, first, last, delta)
		}
		if last > middle {
			add(node*2+1, middle+1, right, first, last, delta)
		}
		maximum[node] = lazy[node] + max(maximum[node*2], maximum[node*2+1])
	}
	for _, e := range events {
		first := sort.SearchInts(unique, e.left)
		last := sort.SearchInts(unique, e.right) - 1
		add(1, 0, len(unique)-2, first, last, e.delta)
		if maximum[1] > 1 {
			return true
		}
	}
	return false
}
