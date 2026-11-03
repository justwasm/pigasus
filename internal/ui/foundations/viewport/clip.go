package viewport

import (
	"sort"
	"strings"
)

// ViewOverflowRows is how far past the fold REF lets the emitted view
// run before it stops emitting rows entirely.
const ViewOverflowRows = 8

// ScreenRows returns the number of visible terminal rows an entry produces
// when written to the body — i.e. 1 + the number of embedded "\n"s.
func ScreenRows(entry string) int {
	if entry == "" {
		return 1
	}
	return 1 + strings.Count(entry, "\n")
}

// ScreenRowCounts builds a screen-row count for every entry of rows.
func ScreenRowCounts(rows []string) []int {
	counts := make([]int, len(rows))
	for i, r := range rows {
		counts[i] = ScreenRows(r)
	}
	return counts
}

// ClipToScreenRows trims visible from the right until its total rendered
// row count fits inside maxRows. Cycle 124.
func ClipToScreenRows(visible []string, screenCounts []int, scrollOff, maxRows int) []string {
	if len(visible) == 0 || maxRows <= 0 {
		return visible
	}
	for {
		total := 0
		for i := range visible {
			idx := scrollOff + i
			if idx >= len(screenCounts) {
				break
			}
			total += screenCounts[idx]
		}
		if total <= maxRows {
			return visible
		}
		visible = visible[:len(visible)-1]
		if len(visible) == 0 {
			return visible
		}
	}
}

// ClipToScreenRowsPrefix is ClipToScreenRows with the counts replaced by
// their prefix sum, so the trim is a binary search instead of a rescan.
func ClipToScreenRowsPrefix(visible []string, prefix []int, scrollOff, maxRows int) []string {
	if len(visible) == 0 || maxRows <= 0 {
		return visible
	}
	if scrollOff < 0 || scrollOff >= len(prefix) {
		return visible
	}
	limit := prefix[scrollOff] + maxRows
	k := sort.Search(len(visible)+1, func(i int) bool {
		idx := scrollOff + i
		if idx >= len(prefix) {
			return true
		}
		return prefix[idx] > limit
	}) - 1
	if k < 0 {
		k = 0
	}
	if k > len(visible) {
		k = len(visible)
	}
	return visible[:k]
}
