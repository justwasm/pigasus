package sidequestion

import (
	"fmt"
	"strings"
	"time"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
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
	if maxLines <= 0 {
		return nil
	}
	if len(ansLines) <= maxLines {
		return ansLines
	}
	if !expanded {
		win := tuiviewport.NewWindow(len(ansLines), 0, maxLines)
		return append(append([]string(nil), win.SliceRows(ansLines)...), "... (truncated)")
	}
	sp := tuiviewport.ScrollPosition{Offset: scroll}
	win := sp.Layout(len(ansLines), maxLines)
	res := append([]string(nil), win.SliceRows(ansLines)...)
	return append(res, fmt.Sprintf("↑/↓ to scroll (%d-%d of %d)", win.Start()+1, win.End(), len(ansLines)))
}
