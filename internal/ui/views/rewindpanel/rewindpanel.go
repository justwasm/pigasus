// Package rewindpanel holds the /rewind turn picker's own vocabulary:
// sizing the visible window, rendering the step rows and formatting the
// footer counter.
//
// # What it does not own
//
// The *policy* — which messages a turn owns, how many a rewind drops
// and what the transcript says afterwards — belongs to the session
// layer (internal/app/session/rewind). This package names a picked
// step and draws it; the session layer decides what the name means.
//
// The picker's *state* — which step the cursor is on, which window
// slice is visible — lives in the panel, in the host. Everything that
// only reads its arguments lives here.
package rewindpanel

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/app/session/rewind"
)

// snippetCap is the number of runes a step row's snippet may hold.
//
// The cap is the row's business: the snippet is the turn's first line,
// cut to fit the row.
const snippetCap = 30

// firstRow takes the first non-empty line of s and caps it at max
// runes.
func firstRow(s string, max int) string {
	for _, line := range strings.SplitN(s, "\n", 2) {
		line = strings.TrimSpace(line)
		if line != "" {
			if r := []rune(line); len(r) > max {
				return string(r[:max-1]) + "…"
			}
			return line
		}
	}
	return ""
}

// stepHeight is the vertical budget /rewind's picker consumes at
// the current terminal height: title (1) + blank (1) + rows + blank (1)
// + counter (1) + keyboard (1) = 5 chrome rows around the list. Matches
// the cycle-189 height-less default of 17 visible rows; falls back to
// the same when h is 0 (test runs, alt-screen-not-yet-settled).
func stepHeight(h int) int {
	if h <= 0 {
		return 17
	}
	const chrome = 5
	v := h - chrome - 1 /* the status row */
	if v < 1 {
		v = 1
	}
	return v
}

// renderStepRows renders one row per turn. The cursor row is drawn in
// brand blue; the most recent turn (index 0) carries a dim-grey
// "(current)" annotation.
//
// The two colour constants are passed in so this package does not need
// to know the panel's palette.
//
// The row's label is the step's own text: the first non-empty line of
// the turn, capped at the row's budget.
func renderStepRows(steps []rewind.Step, cursor int, blueOpen, blueClose, gray238, gray238Close string) []string {
	allRows := make([]string, len(steps))
	for i, s := range steps {
		label := fmt.Sprintf("Turn %d  %s", i+1, firstRow(s.Content, snippetCap))
		if i == 0 {
			label += gray238 + "  (current)" + gray238Close
		}
		if i == cursor {
			allRows[i] = blueOpen + "> " + label + blueClose
		} else {
			allRows[i] = "  " + label
		}
	}
	return allRows
}

// stepCounter renders the dim gray "(a–b of K turns)" counter the
// /rewind picker shows in its footer when the conversation has more steps
// than fit in the visible window. Same colour code as the viewer
// foundation's counter but the noun is "turns" — the picker never has
// documents.
func stepCounter(start, end, total int, dimOpen, dimClose string) string {
	return dimOpen + fmt.Sprintf("  (%d–%d of %d turns)", start, end, total) + dimClose
}
