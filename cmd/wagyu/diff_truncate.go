// diff_truncate.go — cycle diff-cjk-cutoff: grapheme-cluster-aware
// long-line truncation for the /diff slash command.
//
// AGY 1.3.6 changelog line 11 closes the gap where the /diff viewer
// used a byte-level slice to cut long lines and so split CJK
// characters (and ZWJ family emoji) at the cutoff edge, producing
// U+FFFD replacement characters where a clean ellipsis was expected.
//
// truncateDiffLine replaces that slice with a grapheme-cluster walk
// using rivo/uniseg. The display width of each cluster is summed via
// the same library so CJK and wide emoji still consume two cells,
// halfwidth forms consume one, and combining / ZWJ sequences consume
// one combined cluster — so a cluster is never broken across the
// ellipsis boundary.
package main

import (
	"strings"

	"github.com/rivo/uniseg"
)

// truncateDiffLine shortens s to at most maxCells terminal cells,
// appending an ASCII "..." ellipsis when truncation occurs. The
// input is returned unchanged when it already fits in maxCells (or
// maxCells is 0, in which case truncation is disabled and the raw
// string is returned — used by tests that need the unmodified diff
// body). When maxCells is 1..3 the ellipsis alone is returned,
// because no cluster can fit alongside the three ellipsis cells.
//
// Cutoff points always land on grapheme cluster boundaries (cycle
// diff-cjk-cutoff, AGY 1.3.6 line 11): a CJK character or a ZWJ
// family-emoji sequence sitting at the edge is never split
// mid-cluster.
func truncateDiffLine(s string, maxCells int) string {
	if maxCells <= 0 {
		return s
	}
	const ellipsis = "..."
	if uniseg.StringWidth(s) <= maxCells {
		return s
	}
	budget := maxCells - len(ellipsis)
	if budget <= 0 {
		return ellipsis
	}
	var b strings.Builder
	g := uniseg.NewGraphemes(s)
	used := 0
	for g.Next() {
		w := g.Width()
		if used+w > budget {
			break
		}
		b.WriteString(g.Str())
		used += w
	}
	b.WriteString(ellipsis)
	return b.String()
}
