package sidequestion

import (
	"fmt"
	"strings"
	"time"
)

// Spinner returns the active Braille spinner character for side-question processing.
func Spinner() string {
	frames := []string{"⣾", "⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽"}
	return frames[int(time.Now().UnixMilli()/100)%len(frames)]
}

// HintText generates the card hint bar joined with " · ":
// "Tab to collapse/expand · ←/→ to navigate · Enter/Esc to dismiss"
func HintText(expanded bool, cardCount int) string {
	var hints []string
	if expanded {
		hints = append(hints, "Tab to collapse")
	} else {
		hints = append(hints, "Tab to expand")
	}
	if cardCount > 1 {
		hints = append(hints, "←/→ to navigate")
	}
	hints = append(hints, "Enter/Esc to dismiss")
	return strings.Join(hints, " · ")
}

// FormatAnswerLines formats and scroll-windows rendered markdown text into lines.
func FormatAnswerLines(styled string, maxLines, scroll int, expanded bool) []string {
	ansLines := strings.Split(styled, "\n")
	if len(ansLines) <= maxLines {
		return ansLines
	}
	if !expanded {
		return append(ansLines[:maxLines], "... (truncated)")
	}
	if scroll < 0 {
		scroll = 0
	}
	maxScroll := len(ansLines) - maxLines
	if scroll > maxScroll {
		scroll = maxScroll
	}
	end := scroll + maxLines
	if end > len(ansLines) {
		end = len(ansLines)
	}
	res := append([]string{}, ansLines[scroll:end]...)
	return append(res, fmt.Sprintf("↑/↓ to scroll (%d-%d of %d)", scroll+1, end, len(ansLines)))
}
