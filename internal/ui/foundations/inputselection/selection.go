// Package inputselection holds the pure geometry of the composer's text
// selection: the line spans of a multi-line value, the byte offset a screen
// column maps to, and the reverse-video highlight painted over the
// composer's rendered rows.
//
// The geometry is host-free. The host (internal/cliapp) owns the state —
// the selection's own offsets and the textarea's cursor — and hands the
// plain values in; every function here is a function of its arguments and
// nothing else.
//
// The package also holds the value arithmetic a selection produces: where
// the cursor lands in a multi-line value (edit.go) and what the
// selection's delete and replace do to the value. The *store* — the
// selection's own offsets — stays with the host, which is where the
// textarea is.
//
// The composer's selection is painted with the transcript's own cue (the
// viewport package's reverse video), so a drag in the composer and a drag
// in the transcript look the same.
package inputselection

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// FindLineSpan returns the byte offsets [start, end) of the given logical
// line within val.
//
// The line index is the logical line number — 0 is the first line, and a
// line index past the last line yields the trailing empty line's span.
func FindLineSpan(val string, targetLine int) (int, int) {
	lineStart := 0
	curLine := 0
	for i := 0; i < len(val); i++ {
		if curLine == targetLine {
			lineStart = i
			break
		}
		if val[i] == '\n' {
			curLine++
			lineStart = i + 1
		}
	}
	lineEnd := len(val)
	if nl := strings.IndexByte(val[lineStart:], '\n'); nl >= 0 {
		lineEnd = lineStart + nl
	}
	return lineStart, lineEnd
}

// ByteOffsetInLine returns the byte offset inside lineText of the display
// column targetCol.
//
// A click that lands in the second half of a wide rune advances past it, so
// the click always lands on the rune's own boundary.
func ByteOffsetInLine(lineText string, targetCol int) int {
	if targetCol <= 0 {
		return 0
	}
	curCol := 0
	byteOff := 0
	for byteOff < len(lineText) {
		r, size := utf8.DecodeRuneInString(lineText[byteOff:])
		w := ansi.StringWidth(string(r))
		if curCol+w > targetCol {
			// If click lands in the second half of a wide rune, advance past it.
			if targetCol >= curCol+w/2+1 {
				byteOff += size
			}
			break
		}
		curCol += w
		byteOff += size
	}
	return byteOff
}

// OffsetAt maps a screen coordinate to a byte offset within value.
//
// promptScreenY is the screen row of the composer's first line, lineCount
// the number of visual lines the composer occupies, and x the screen column
// of the click. The prompt itself occupies two display cells ("> " on the
// first line, "  " on the continuation lines).
func OffsetAt(value string, lineCount, promptScreenY, x, y int) int {
	if len(value) == 0 {
		return 0
	}
	lineIdx := y - promptScreenY
	if lineIdx < 0 {
		return 0
	}
	if lineIdx >= lineCount {
		return len(value)
	}

	lineStart, lineEnd := FindLineSpan(value, lineIdx)
	lineText := value[lineStart:lineEnd]
	// The prompt occupies 2 display cells ("> " on line 0, "  " on continuation lines).
	targetCol := x - 2
	return lineStart + ByteOffsetInLine(lineText, targetCol)
}

// HighlightSingleLine paints the selected bytes of one rendered row in
// reverse video.
//
// line is the rendered row, val the composer's plain value, [lineStart,
// lineEnd) the row's span in val, and [start, end) the selection's span in
// val. A row the selection does not reach is returned untouched.
func HighlightSingleLine(line, val string, lineStart, lineEnd, start, end int) string {
	selStartInLine := start
	if selStartInLine < lineStart {
		selStartInLine = lineStart
	}
	selEndInLine := end
	if selEndInLine > lineEnd {
		selEndInLine = lineEnd
	}

	if selStartInLine < selEndInLine {
		lineText := val[lineStart:lineEnd]
		offsetFromLine := selStartInLine - lineStart
		offsetToLine := selEndInLine - lineStart

		prefixText := lineText[:offsetFromLine]
		selText := lineText[offsetFromLine:offsetToLine]

		fromCell := 2 + ansi.StringWidth(prefixText)
		toCell := fromCell + ansi.StringWidth(selText)

		return tuiviewport.ApplySelectionHighlightToRow(line, fromCell, toCell)
	}
	return line
}

// ApplyHighlight paints the selected bytes of the composer's rendered rows
// in reverse video.
//
// inputView is the rendered composer, val the composer's plain value, and
// [start, end) the selection's byte range in val. The range is normalized
// first, so a reversed range selects the same bytes a forward one does.
func ApplyHighlight(inputView, val string, start, end int) string {
	if start > end {
		start, end = end, start
	}
	if start >= end {
		return inputView
	}
	if start >= len(val) {
		return inputView
	}
	if end > len(val) {
		end = len(val)
	}

	lines := strings.Split(inputView, "\n")
	hasTrailingNewline := strings.HasSuffix(inputView, "\n")
	if hasTrailingNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	lineStart := 0
	for lineIdx := 0; lineIdx < len(lines); lineIdx++ {
		lineEnd := len(val)
		if nl := strings.IndexByte(val[lineStart:], '\n'); nl >= 0 {
			lineEnd = lineStart + nl
		}

		lines[lineIdx] = HighlightSingleLine(lines[lineIdx], val, lineStart, lineEnd, start, end)

		if lineEnd < len(val) {
			lineStart = lineEnd + 1
		} else {
			lineStart = len(val)
		}
	}

	res := strings.Join(lines, "\n")
	if hasTrailingNewline {
		res += "\n"
	}
	return res
}
