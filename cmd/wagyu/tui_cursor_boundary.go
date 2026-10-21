// tui_cursor_boundary.go — cursor-line classification helpers for the
// prompt's Up/Down key dispatch.
//
// Cycle multiline-cursor-history-boundary: the v1.3.5 binary only
// recalls history on Up/Down when the cursor sits at the boundary of
// the input box — Up on the first visual line, Down on the last. On
// any other line of a multi-line prompt, Up/Down move the cursor
// within the textarea (cycle 71 reuses charm.land/bubbles/v2's
// built-in LinePrevious / LineNext key bindings for that).
//
// These helpers are the source of truth for the rule. tui_run.go's
// key handler asks isOnFirstLine / isOnLastLine about the cursor's
// position; the unit tests in tui_cursor_boundary_test.go drive the
// helpers directly with a variety of (value, byte-offset) inputs to
// pin the boundary semantics.
package main

import (
	"strings"
	"unicode/utf8"

	tas "charm.land/bubbles/v2/textarea"
)

// cursorLine returns the 0-indexed visual-line number that contains
// the byte offset cursor within value. A value with no newline
// characters is on line 0 for every byte position in
// [0, len(value)]. Out-of-range cursor values are clamped to
// [0, len(value)] before counting.
//
// The cursor is treated as a byte offset because that is the unit
// the bubbles/v2 textinput API exposes (textinput.Model.Cursor()
// returns an int byte position). The wagyu clone's m.input is a
// textarea, which only exposes Line()/Column(); inputCursorByteOffset
// below bridges to the byte-position interface so this rule has a
// single, testable source of truth regardless of which input
// component the prompt is wired to.
func cursorLine(value string, cursor int) int {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	line := 0
	for i := 0; i < cursor; i++ {
		if value[i] == '\n' {
			line++
		}
	}
	return line
}

// isOnFirstLine reports whether the byte offset cursor within value
// lies on the first visual line — i.e. there is no newline before
// it. A value with no newlines and an empty value report true for
// every cursor position.
func isOnFirstLine(value string, cursor int) bool {
	return cursorLine(value, cursor) == 0
}

// isOnLastLine reports whether the byte offset cursor within value
// lies on the last visual line — i.e. there is no newline at or
// after the cursor position. A value with no newlines and an
// empty value report true for every cursor position.
//
// The cursor may point one byte past the end of the row (a valid
// cursor position when the cursor is at end-of-row in edit mode); the
// slice value[cursor:] is then empty and has no newline, so this
// function correctly returns true.
func isOnLastLine(value string, cursor int) bool {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	return strings.IndexByte(value[cursor:], '\n') == -1
}

// inputCursorByteOffset returns the byte offset of the textarea
// cursor in m.Value(). The bubbles/v2 textarea only exposes the
// cursor as a (Line(), Column()) rune pair, so we reconstruct the
// byte position by walking the value's newlines to find the start
// of the cursor's row, then converting the rune column to a byte
// offset within that row.
//
// Used by tui_run.go's Up/Down handler to ask isOnFirstLine /
// isOnLastLine. Mirrors the textinput.Model.Cursor() int byte
// position the AGY binary's input component historically exposed.
func inputCursorByteOffset(m tas.Model) int {
	value := m.Value()
	line := m.Line()
	col := m.Column()

	// Walk newlines to find the byte offset of the start of the
	// cursor's row. Loop terminates early once we have seen enough
	// newlines; the cursor's row otherwise starts at the byte after
	// the (line-1)-th newline (or 0 when line == 0).
	rowStart := 0
	rowsWalked := 0
	for i := 0; i < len(value); i++ {
		if rowsWalked == line {
			rowStart = i
			break
		}
		if value[i] == '\n' {
			rowsWalked++
			rowStart = i + 1
		}
	}
	if rowsWalked < line {
		// Cursor row is past end of value. Should not happen for a
		// well-formed textarea (Line() <= len(value)-1 by
		// construction), but guard against it so the helper always
		// returns a byte offset inside [0, len(value)].
		return len(value)
	}

	// Find the end of the cursor's row (the next newline or EOF).
	rowEnd := len(value)
	if nl := strings.IndexByte(value[rowStart:], '\n'); nl >= 0 {
		rowEnd = rowStart + nl
	}

	// Convert rune column to byte offset within the row. UTF-8
	// runes are 1-4 bytes each; utf8.DecodeRuneInString walks one
	// rune at a time so multi-byte characters are counted
	// correctly.
	rest := value[rowStart:rowEnd]
	byteOff := 0
	for i := 0; i < col && i < len(rest); {
		_, size := utf8.DecodeRuneInString(rest[i:])
		byteOff += size
		i += size
	}
	return rowStart + byteOff
}
