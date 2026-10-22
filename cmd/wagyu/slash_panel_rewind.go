package main

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// rewindStep is one row in /rewind's picker. userIdx is the m.session
// index of the user message that anchors the step; endIdx is the slice
// index AFTER the last message the step owns (one past the trailing
// assistant / tool-result chain).
type rewindStep struct {
	userIdx int
	endIdx  int
	snippet string
}

// rewindSteps breaks a session into turn steps starting with a user message.
func rewindSteps(messages []backend.Message) []rewindStep {
	var steps []rewindStep
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
			steps = append(steps, rewindStep{
				userIdx: i,
				endIdx:  end,
				snippet: firstRow(messages[i].Content, 30),
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

// firstRow takes the first non-empty line of s and caps it at max runes.
func firstRow(s string, max int) string {
	for _, line := range strings.SplitN(s, "\n", 2) {
		line = strings.TrimSpace(line)
		if line != "" {
			if r := []rune(line); len(r) > max {
				return string(r[:max-1]) + "\u2026"
			}
			return line
		}
	}
	return ""
}

// rewindStepHeight is the vertical budget /rewind's picker consumes at
// the current terminal height: title (1) + blank (1) + rows + blank (1)
// + counter (1) + keyboard (1) = 5 chrome rows around the list. Matches
// the cycle-189 height-less default of 17 visible rows; falls back to
// the same when m.height is 0 (test runs, alt-screen-not-yet-settled).
func rewindStepHeight(h int) int {
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

// panelRewindLines renders the /rewind picker. Cycle 189 shipped the
// empty-state form (literal `(current)` row); cycle 222 fills in the
// non-empty form — one row per user-anchored turn, newest first, with
// the cursor row in brand blue and a `(current)` annotation on the
// most recent turn.
func (m *runMode) panelRewindLines() []string {
	steps := rewindSteps(m.session)
	if len(steps) == 0 {
		// Cycle 189 empty form — preserved unchanged so the same
		// TestPanelRewindEmptyLayout golden file keeps passing.
		return []string{
			panelTitleOpen + "Rewind Conversation" + panelTitleClose,
			"",
			cBlueOpen + "> (current)" + cBlueClose,
			"",
			panelKeyboardHint("↑/↓", "Navigate", "enter", "Select", "esc", "Cancel"),
		}
	}
	visible := rewindStepHeight(m.height)
	total := len(steps)
	if visible > total {
		visible = total
	}
	// Clamp window + cursor to the loaded state. openSlashPanel resets
	// both to 0; tests may have advanced them.
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if m.panel.cursor < 0 {
		m.panel.cursor = 0
	}
	if max := total - visible; m.panel.window > max && max >= 0 {
		m.panel.window = max
	}
	if m.panel.cursor > total-1 {
		m.panel.cursor = total - 1
	}
	window := m.panel.window

	// Pre-render each step row.
	allRows := make([]string, total)
	for i, s := range steps {
		label := fmt.Sprintf("Turn %d  %s", i+1, s.snippet)
		if i == 0 {
			// Cursor's "current turn" annotation — placed in dim gray
			// 238 (the same palette the (current) flag carries in the
			// /model picker). Only the first step carries it; for a
			// multi-pane capture a second `(current)` would just be
			// visual noise.
			label += panelGray238 + "  (current)" + panelGray238C
		}
		if i == m.panel.cursor {
			allRows[i] = cBlueOpen + "> " + label + cBlueClose
		} else {
			allRows[i] = "  " + label
		}
	}

	lines := []string{
		panelTitleOpen + "Rewind Conversation" + panelTitleClose,
		"",
	}
	lines = append(lines, allRows[window:window+visible]...)
	lines = append(lines, "")
	if total > visible {
		lines = append(lines, rewindStepCounter(window+1, window+visible, total))
	}
	lines = append(lines, panelKeyboardHint("↑/↓", "Navigate",
		"enter", "Select", "esc", "Cancel"))
	return lines
}

// rewindStepCounter renders the dim gray "(a–b of K turns)" counter the
// /rewind picker shows in its footer when the conversation has more steps
// than fit in the visible window. Same colour code as viewerCounter (line
// 2164) but the noun is "turns" — the picker never has documents.
func rewindStepCounter(start, end, total int) string {
	return cDimGrayOpen + fmt.Sprintf("  (%d\u2013%d of %d turns)", start, end, total) + cDimGrayClose
}

// panelRewindPickStep is the cycle-222 Enter handler for /rewind. The
// safe stub the cycle brief allowed: the picker emits a `Rewound to
// step N` line via slashRespond's REF-style echo format, the panel
// closes silently (same exit-notice=false set as /mcp /feedback), and
// m.session is truncated so a follow-up prompt no longer carries the
// dropped user+assistant pair. Doing the transcript edit (instead of a
// no-op) keeps the existing /rewind slash command and the picker in
// step — rewindSession pops only the trailing pair, while this routine
// pops trailing pairs until the picked step's user message is the
// trailing user message. Returns the trimmed count for the echo line.
func (m *runMode) panelRewindPickStep() int {
	steps := rewindSteps(m.session)
	if len(steps) == 0 {
		// Empty form — Enter is a no-op (matches cycle-189's "no rewind
		// target" comment). Esc still handles silent close.
		return 0
	}
	cur := m.panel.cursor
	if cur < 0 || cur >= len(steps) {
		cur = 0
	}
	// The picked step's NEW end boundary — keep everything up to and
	// including the step's trailing assistant / tool-result chain (one
	// past the user message). EndIdx is the slice index immediately
	// after the last message the step owns (the trailing assistant
	// message in the simple user/assistant case).
	target := steps[cur].endIdx
	removed := len(m.session) - target
	if removed <= 0 {
		// Already at or before the picked step (cursor on the current
		// turn, which is the default). No-op: no truncation, no echo.
		return 0
	}
	m.session = m.session[:target]
	if cur == 0 {
		m.slashRespond("/rewind",
			fmt.Sprintf("Rewound to step %d (current, no messages removed)", cur+1))
	} else {
		m.slashRespond("/rewind",
			fmt.Sprintf("Rewound to step %d (%d message(s) removed)", cur+1, removed))
	}
	return removed
}
