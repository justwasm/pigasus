// Package rewindpanel holds the pure parts of the /rewind turn picker:
// breaking a session into turn steps, capping the snippet, sizing the
// visible window, rendering the step rows and formatting the footer
// counter.
//
// The picker's *state* — which step the cursor is on, which window
// slice is visible — lives in the panel, in cmd/wagyu. Everything that
// only reads its arguments lives here.
package rewindpanel

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// Step is one row in /rewind's picker. UserIdx is the session
// index of the user message that anchors the step; EndIdx is the slice
// index AFTER the last message the step owns (one past the trailing
// assistant / tool-result chain).
type Step struct {
	UserIdx int
	EndIdx  int
	Snippet string
}

// Steps breaks a session into turn steps starting with a user message.
func Steps(messages []backend.Message) []Step {
	var steps []Step
	// Walk backwards so we know the most-recent end index first; capture
	// the index AFTER the trailing assistant (or after the user message
	// itself when no assistant has replied yet). Then sweep back: every
	// time we encounter a user-role message we record it as a step whose
	// "end" boundary is the immediately preceding user's start.
	if len(messages) == 0 {
		return nil
	}
	end := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			steps = append(steps, Step{
				UserIdx: i,
				EndIdx:  end,
				Snippet: FirstRow(messages[i].Content, 30),
			})
			end = i
		}
	}
	// The `end = i` above resets to *before* the user message, so the
	// remaining assistant/tool messages before the first user message are
	// silently dropped from the picker — that matches REF, which doesn't
	// surface orphan leading assistant rows. The result is newest-first.
	return steps
}

// FirstRow takes the first non-empty line of s and caps it at max runes.
func FirstRow(s string, max int) string {
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

// StepHeight is the vertical budget /rewind's picker consumes at
// the current terminal height: title (1) + blank (1) + rows + blank (1)
// + counter (1) + keyboard (1) = 5 chrome rows around the list. Matches
// the cycle-189 height-less default of 17 visible rows; falls back to
// the same when h is 0 (test runs, alt-screen-not-yet-settled).
func StepHeight(h int) int {
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

// RenderStepRows renders one row per turn. The cursor row is drawn in
// brand blue; the most recent turn (index 0) carries a dim-grey
// "(current)" annotation.
//
// The two colour constants are passed in so this package does not need
// to know the panel's palette.
func RenderStepRows(steps []Step, cursor int, blueOpen, blueClose, gray238, gray238Close string) []string {
	allRows := make([]string, len(steps))
	for i, s := range steps {
		label := fmt.Sprintf("Turn %d  %s", i+1, s.Snippet)
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

// StepCounter renders the dim gray "(a–b of K turns)" counter the
// /rewind picker shows in its footer when the conversation has more steps
// than fit in the visible window. Same colour code as viewerCounter but
// the noun is "turns" — the picker never has documents.
func StepCounter(start, end, total int, dimOpen, dimClose string) string {
	return dimOpen + fmt.Sprintf("  (%d–%d of %d turns)", start, end, total) + dimClose
}

// RewindSession pops the trailing user/assistant pair from sess and
// returns the number of messages removed.
func RewindSession(sess []backend.Message) int {
	if len(sess) == 0 {
		return 0
	}
	removed := 0
	if len(sess) > 0 && sess[len(sess)-1].Role == "assistant" {
		sess = sess[:len(sess)-1]
		removed++
	}
	if len(sess) > 0 && sess[len(sess)-1].Role == "user" {
		sess = sess[:len(sess)-1]
		removed++
	}
	return removed
}

