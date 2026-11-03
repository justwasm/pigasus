// tabaligned.go — the tab-separated content block, laid out as columns.
//
// The print mode's slash commands answer with tab-separated rows, and a
// raw tab collapses in the transcript's renderer. The layout is a pure
// function of the text: it measures every column's rune width, pads the
// cell to the widest one plus two spaces, and trims the row's trailing
// spaces away.
//
// The widths are measured in RUNES, not bytes, so a multi-byte rune
// (an accented letter, an arrow, a CJK glyph) counts as one column.
//
// The layout belongs here because it is the transcript's row vocabulary:
// it decides what a content row looks like, and it reads no host state.

package rows

import (
	"strings"
	"unicode/utf8"
)

// TabAlignedRows lays the tab-separated text out as aligned columns and
// returns the rows.
//
// An empty text produces no rows at all. A text that ends with a
// newline loses it first, so the trailing newline never becomes a row
// of its own.
func TabAlignedRows(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	cells := make([][]string, len(lines))
	var widths []int
	for i, line := range lines {
		cells[i] = strings.Split(line, "\t")
		for j, cell := range cells[i] {
			if j >= len(widths) {
				widths = append(widths, 0)
			}
			if w := utf8.RuneCountInString(cell); w > widths[j] {
				widths[j] = w
			}
		}
	}
	out := make([]string, 0, len(cells))
	for _, row := range cells {
		var b strings.Builder
		for j, cell := range row {
			b.WriteString(cell)
			if j < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-utf8.RuneCountInString(cell)+2))
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}
