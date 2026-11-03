// separator.go — the rule between two turns.

package transcript

import (
	"github.com/justwasm/pigasus/palette"
)

// TurnSeparator returns the horizontal rule drawn between two turns.
//
// The rule is 60 columns wide unless the terminal is narrower, in which
// case it is drawn three columns short of the width — and, below that,
// at the full width so it never wraps.
func TurnSeparator(width int) string {
	w := 60
	if width > 0 && width-3 < w {
		w = width - 3
		if w < 10 {
			w = width
		}
	}
	return palette.Rule(w)
}
