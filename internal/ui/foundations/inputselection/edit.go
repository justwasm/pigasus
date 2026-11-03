// edit.go — the composer's value arithmetic: where the cursor lands in
// a multi-line value, and what a selection's delete and replace
// produce.
//
// The arithmetic is host-free, exactly like the geometry in
// selection.go: the host owns the textarea and the selection's own
// offsets, and hands the plain values in.
package inputselection

import "unicode/utf8"

// CursorPosition returns the logical line and the rune column the given
// byte offset sits at in val.
//
// The offset is clamped into the value: a negative offset lands at the
// start, an offset past the end lands at the end. The column is
// counted in runes, so a wide rune counts as one column — the same
// count the composer's own cursor uses.
func CursorPosition(val string, offset int) (line, col int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(val) {
		offset = len(val)
	}
	lineStart := 0
	for i := 0; i < offset; i++ {
		if val[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, utf8.RuneCountInString(val[lineStart:offset])
}

// DeleteRange returns val with the bytes in [start, end) removed.
//
// The range is normalised first (swap when start > end), so a reversed
// range removes the same bytes a forward one does — the same rule
// ApplyHighlight follows. A range that reaches past the end of the
// value leaves the value alone and reports false; the caller decides
// what an out-of-bounds selection means.
func DeleteRange(val string, start, end int) (string, bool) {
	if start > end {
		start, end = end, start
	}
	if start > len(val) || end > len(val) {
		return val, false
	}
	return val[:start] + val[end:], true
}

// ReplaceRange returns val with the bytes in [start, end) replaced by
// replacement.
//
// The range is normalised first, exactly as DeleteRange does.
func ReplaceRange(val string, start, end int, replacement string) (string, bool) {
	if start > end {
		start, end = end, start
	}
	if start > len(val) || end > len(val) {
		return val, false
	}
	return val[:start] + replacement + val[end:], true
}
