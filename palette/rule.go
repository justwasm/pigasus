package palette

import "strings"

// Rule returns `<open>─<close>` — one cell of the horizontal rule the
// reference draws between the transcript and the prompt and again
// between the prompt and the status row. width is in runes.
func Rule(width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(SeparatorOpen) + 3*width + len(SeparatorClose))
	b.WriteString(SeparatorOpen)
	for i := 0; i < width; i++ {
		b.WriteString("─")
	}
	b.WriteString(SeparatorClose)
	return b.String()
}
