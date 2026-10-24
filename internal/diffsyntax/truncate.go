package diffsyntax

import (
	"strings"

	"github.com/rivo/uniseg"
)

// TruncateLine shortens s to at most maxCells terminal cells,
// appending an ASCII "..." ellipsis when truncation occurs.
func TruncateLine(s string, maxCells int) string {
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
