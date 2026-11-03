package viewport

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// IsPromptTurnSeparator reports whether row is the thin rule that
// separates two prompt turns in the transcript.
//
// A separator is a row whose visible text is made of nothing but the
// box-drawing rule character. The row's ANSI escapes are stripped
// before the test, so a coloured rule still counts; a row that merely
// *contains* the rule character (a table border, a sentence) does not.
func IsPromptTurnSeparator(row string) bool {
	stripped := strings.TrimSpace(ansi.Strip(row))
	return len(stripped) > 0 && strings.Trim(stripped, "─") == ""
}

// PromptTurnStart returns the index of the first row of the prompt turn
// that promptIdx belongs to.
//
// The turn starts at the blank line that precedes the turn's separator
// rule, so the rule and the prompt row travel with the turn when the
// viewport scrolls to it. An index outside rows — or the first row
// itself — has no turn to start, and the answer is 0.
func PromptTurnStart(rows []string, promptIdx int) int {
	if promptIdx <= 0 || promptIdx >= len(rows) {
		return 0
	}
	start := promptIdx
	for start > 0 && IsPromptTurnSeparator(rows[start-1]) {
		start--
	}
	if start > 0 && strings.TrimSpace(rows[start-1]) == "" {
		start--
	}
	return start
}

// PromptTurnVisible reports whether the row idx is in view at the given
// scroll offset.
//
// A row at or below the scroll offset is visible. A row above it is
// still visible when it belongs to the prompt turn that the viewport is
// inside: the turn's separator rule and its prompt row bracket the
// offset, so the row travels with them.
func PromptTurnVisible(rows []string, idx, scroll int, userPromptRows []int) bool {
	if scroll <= idx {
		return true
	}
	for _, p := range userPromptRows {
		if p > idx {
			start := PromptTurnStart(rows, p)
			if scroll >= start && scroll <= p {
				return true
			}
			break
		}
	}
	return false
}
