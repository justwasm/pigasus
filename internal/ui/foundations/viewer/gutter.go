// gutter.go — the numbered-line gutter the documentary viewers share.
//
// # What it is
//
// /artifact and the /debug reader both show a body of text the user reads
// line by line, with the line's number in a gutter, a cursor marking the
// line the keys act on, and a counter under the window. The shapes were
// measured from the reference (see artifactpanel's package comment):
//
//	>   1      the cursor's line
//	    2      any other line
//	> 102      a number wider than the cell grows it
//
// so the text always starts at the same column, and `j j j` moves the
// cursor without moving the window.
//
// # Why here
//
// The arithmetic is one column's worth of spaces and one subtraction, and
// that is exactly the kind of thing that gets copied: the /debug reader
// needs the same gutter, and a second copy would drift the first time a
// width changed — the same class of bug that put "tools 3 3.8 KB" three
// columns left of its members. Both callers already import this package,
// so it is the natural owner.
//
// # Deliberately not here
//
// The cursor's colour and the document's rows stay with the panels: one
// draws a file, the other draws a wrapped body, and a shared row builder
// would have to be told which. What is shared is the *geometry*.
package viewer

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Gutter constants, measured from the reference's frames.
const (
	// GutterNumberWidth is the width of the number cell, right-aligned.
	// A number wider than this grows the cell rather than being cut.
	GutterNumberWidth = 4

	// GutterGap is the run of spaces between the number cell and the
	// text, so the text always starts at the same column.
	GutterGap = 6

	// GutterCursorWidth is the cursor column's width: ">" or a space.
	GutterCursorWidth = 1
)

// GutterIndent is how many cells the gutter occupies before the text, for
// a document of total lines.
//
// A caller uses it to size the body it wraps: the text has this many
// cells fewer than the panel's width.
func GutterIndent(total int) int {
	return GutterCursorWidth + NumberCellWidth(total) + GutterGap
}

// NumberCellWidth is the number cell's width for a document of total
// lines: the constant, grown for a document whose count needs more
// digits.
func NumberCellWidth(total int) int {
	w := GutterNumberWidth
	if n := len(strconv.Itoa(total)); n > w {
		w = n
	}
	return w
}

// LineNumber renders n right-aligned in the number cell for a document of
// total lines.
func LineNumber(n, total int) string {
	num := strconv.Itoa(n)
	pad := NumberCellWidth(total) - len(num)
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + num
}

// SelectionIndent is how many cells of a row are gutter rather than text,
// for a document of total lines: the cursor column, the number cell and
// the gap.
//
// It is GutterIndent, and it exists under this name because the two
// readers of a selection ask a different question of the same number. A
// copy that starts inside the gutter must not carry the line number: the
// number is the viewer's, not the document's, and pasting "   1      " in
// front of every line is how a copied artifact stops being the artifact.
func SelectionIndent(total int) int { return GutterIndent(total) }

// CursorMark is the cursor column: mark when this row is the cursor's,
// a space otherwise.
//
// The mark is the caller's, because it is styled with the caller's
// palette; what this function owns is the column's width.
func CursorMark(isCursor bool, mark string) string {
	if !isCursor {
		return strings.Repeat(" ", GutterCursorWidth)
	}
	if w := ansi.StringWidth(mark); w > GutterCursorWidth {
		return ansi.Truncate(mark, GutterCursorWidth, "")
	} else if w < GutterCursorWidth {
		return mark + strings.Repeat(" ", GutterCursorWidth-w)
	}
	return mark
}

// GutterRow renders one document line's gutter: the cursor column, the
// number, and the gap that puts the text at a fixed column.
//
// The text is the caller's to append, because whether it is styled and how
// it was wrapped is the caller's business.
func GutterRow(lineNo, total int, isCursor bool, mark string) string {
	return CursorMark(isCursor, mark) + LineNumber(lineNo, total) +
		strings.Repeat(" ", GutterGap)
}

// PositionCounter renders the position row of a line-cursor viewer: the
// scroll percentage, the cursor's line, and the visible range.
//
//	[0%  L4  1-28/102]
//
// The percent tracks the window and the `L` tracks the cursor, which is
// the split that makes "the cursor moved, the window did not" legible.
//
// It is not Counter: that one is the plain "(a–b of K lines)" row six
// panels already print, and its shape must not change.
func PositionCounter(cursorLine, start, end, total int) string {
	pct := 0
	if total > 0 {
		maxStart := total - (end - start + 1)
		if maxStart < 0 {
			maxStart = 0
		}
		switch {
		case maxStart == 0:
			pct = 0
		case start-1 >= maxStart:
			pct = 100
		default:
			pct = (start - 1) * 100 / maxStart
		}
	}
	return "[" + strconv.Itoa(pct) + "%  L" + strconv.Itoa(cursorLine) + "  " +
		strconv.Itoa(start) + "-" + strconv.Itoa(end) + "/" + strconv.Itoa(total) + "]"
}

// WindowForCursor returns the scroll offset that keeps the cursor's line
// visible.
//
// The window follows the cursor only when the cursor would leave it, so
// moving inside the window does not scroll at all. This is the reference's
// own rule (the `j j j` frame shows the cursor at L4 with the range still
// 1-28) and it is what makes a line cursor feel different from a
// viewport scroll.
func WindowForCursor(window, line, visible, total int) int {
	if visible < 1 {
		visible = 1
	}
	if line < window {
		window = line
	}
	if line >= window+visible {
		window = line - visible + 1
	}
	maxOffset := total - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if window > maxOffset {
		window = maxOffset
	}
	if window < 0 {
		window = 0
	}
	return window
}

// ExpandTabs replaces tabs with spaces to the next tab stop, counting cells
// the way the terminal will.
//
// A tab's width depends on the column it starts at, which the terminal
// would resolve differently once the gutter's SGR spans are in the row;
// expanding it here keeps the text at the column the gutter left for it.
func ExpandTabs(line string, tabWidth int) string {
	if !strings.ContainsRune(line, '\t') {
		return line
	}
	if tabWidth < 1 {
		tabWidth = 8
	}
	var b strings.Builder
	b.Grow(len(line) + tabWidth)
	col := 0
	for _, r := range line {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col += ansi.StringWidth(string(r))
	}
	return b.String()
}
