package codesearch

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/palette"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// WidthInset is how much narrower than the terminal the list rows and the
// block separator are.
const WidthInset = 4

// FixedRows is the panel's non-list row count: title, `Found …`, counter,
// blank, `Keyboard:`, trailing blank, status row.
const FixedRows = 7

const (
	budgetNum = 11
	budgetDen = 15
)

// Style escapes for the codesearch panel.
const (
	HeaderOpen  = "\x1b[4m\x1b[38;5;32m"
	HeaderReset = "\x1b[0m"
	RuleOpen    = "\x1b[38;2;238;232;213m"
	ErrorOpen   = "\x1b[38;5;166m"
)

var (
	wordMatch = [2]string{"match", "matches"}
	wordFile  = [2]string{"file", "files"}
)

// SpinnerFrames is the Braille set used during search streaming.
var SpinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// Hit is one matching line plus neighbouring context lines.
type Hit struct {
	File    string // slash-separated path relative to the search root
	Line    int    // 1-based line number
	Col     int    // 1-based rune column of the first match
	Prev    string // line-1 text
	Text    string // the matching line, verbatim
	Next    string // line+1 text
	HasPrev bool
	HasNext bool
	MS, ME  int // first match span, in runes
}

// HitFromMatch reshapes a Match into a Hit with lazy context.
func HitFromMatch(m Match) Hit {
	return Hit{
		File: m.File,
		Line: m.Line,
		Col:  m.Col,
		Text: m.Text,
		MS:   0,
		ME:   len([]rune(m.Text)),
	}
}

// StatusRow renders the status description line for codesearch.
func StatusRow(state string, hits, files, spinner int) (string, bool) {
	switch {
	case state == StateRunning && hits == 0:
		glyph := SpinnerFrames[spinner%len(SpinnerFrames)]
		return "  " + glyph + "  " + palette.DimGrayOpen + "Searching\u2026  [Esc to cancel]" + palette.DimGrayClose, true
	case state == StateRunning:
		return palette.DimGrayOpen + fmt.Sprintf("Found %s in %s.  [Esc to cancel]",
			tuilib.CountLabel(hits, wordMatch),
			tuilib.CountLabel(files, wordFile)) + palette.DimGrayClose, false
	case hits == 0:
		return palette.DimGrayOpen + "No results" + palette.DimGrayClose, true
	default:
		lbl := fmt.Sprintf("%s in %s.",
			tuilib.CountLabel(hits, wordMatch),
			tuilib.CountLabel(files, wordFile))
		var verb string
		switch state {
		case StateCancelled:
			verb = "Cancelled after " + lbl
		case StateFailed:
			verb = "Partial: " + lbl
		default:
			verb = "Found " + lbl
		}
		return palette.DimGrayOpen + verb + "  [Enter on row to open]" + palette.DimGrayClose, false
	}
}

// ListBudget calculates available row budget based on screen height.
func ListBudget(height int) int {
	if height <= 0 {
		return 0
	}
	b := height*budgetNum/budgetDen - FixedRows
	if b < 1 {
		b = 1
	}
	return b
}

// HeaderRow renders an underlined file header.
func HeaderRow(file string) string {
	return HeaderOpen + file + HeaderReset
}

// RuleRow renders a dashed between-block separator.
func RuleRow(width int) string {
	n := width - WidthInset
	if n < 1 {
		n = 1
	}
	return RuleOpen + strings.Repeat("─", n) + palette.DimGrayClose
}

// Fill lays out hits[start:] while they fit the row budget and returns the
// exclusive end index and rows.
func Fill(hits []Hit, start, cursor, budget, width int) (int, []string) {
	if start >= len(hits) {
		return start, nil
	}
	var rows []string
	used := 0
	end := start
	prevFile := ""
	for i := start; i < len(hits); i++ {
		h := hits[i]
		newFile := i == start || h.File != prevFile
		var head []string
		if i == start {
			head = append(head, HeaderRow(h.File), RuleRow(width))
		} else if newFile {
			head = append(head, "", HeaderRow(h.File), RuleRow(width))
		} else {
			head = append(head, RuleRow(width))
		}
		cFlag := 0
		if i == cursor {
			cFlag = 1
		}
		body := BlockRows(h, cFlag, width)
		add := len(head) + len(body)
		if i > start && budget > 0 && used+add > budget {
			break
		}
		rows = append(rows, head...)
		rows = append(rows, body...)
		used += add
		if newFile {
			prevFile = h.File
		}
		end = i + 1
	}
	return end, rows
}

// WindowStart clamps the window offset to a renderable item index.
func WindowStart(window, cursor, total int) int {
	if total == 0 {
		return 0
	}
	start := window
	if start < 0 {
		start = 0
	}
	if start > cursor {
		start = cursor
	}
	if start >= total {
		start = total - 1
	}
	return start
}

// Follow re-anchors the window so the cursor is inside the rendered range.
func Follow(window, cursor, height, width int, hits []Hit) int {
	start := WindowStart(window, cursor, len(hits))
	if cursor < start {
		return cursor
	}
	end, _ := Fill(hits, start, cursor, ListBudget(height), width)
	if cursor >= end {
		return cursor
	}
	return window
}

