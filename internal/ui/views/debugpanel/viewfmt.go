// viewfmt.go — the two number formats the inspector's views print.
//
// The token format is not here: it is internal/core/tokenfmt's, because
// the compaction label and the print path already print it and a second
// implementation would be a second answer to "what does 184000 look
// like". What is left is the byte format, which only these views need.
package debugpanel

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// HumanBytes renders a byte count the way the views print it: "812 B",
// "9.6 KB", "1.2 MB".
//
// A space before the unit keeps the column readable next to a token
// count, which carries its own suffix ("3 271 tok").
func HumanBytes(n int) string {
	switch {
	case n <= 0:
		return "0 B"
	case n < 1024:
		return strconv.Itoa(n) + " B"
	case n < 1024*1024:
		return trimZero(float64(n)/1024, 1) + " KB"
	default:
		return trimZero(float64(n)/(1024*1024), 1) + " MB"
	}
}

// trimZero formats v with `places` decimals and drops a trailing ".0",
// so 9.0 KB reads as "9 KB" and 9.56 KB as "9.6 KB".
func trimZero(v float64, places int) string {
	s := strconv.FormatFloat(v, 'f', places, 64)
	if strings.HasSuffix(s, ".0") {
		return strings.TrimSuffix(s, ".0")
	}
	return s
}

// padLeft right-aligns s in a column of the given width, leaving a
// column that is already wider than the width alone.
func padLeft(s string, width int) string {
	if width <= len(s) {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// padRight left-aligns s in a column of the given width, leaving a value
// that is already wider alone. s must be plain text: a styled string is
// measured in cells, and its escapes are not cells.
func padRight(s string, width int) string {
	if n := ansi.StringWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
