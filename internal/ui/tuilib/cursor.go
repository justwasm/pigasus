package tuilib

import (
	"strings"
	"unicode/utf8"

	tas "charm.land/bubbles/v2/textarea"
)

// CursorLine returns the 0-indexed visual-line number that contains
// the byte offset cursor within value.
func CursorLine(value string, cursor int) int {
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

// IsOnFirstLine reports whether the byte offset cursor within value
// lies on the first visual line.
func IsOnFirstLine(value string, cursor int) bool {
	return CursorLine(value, cursor) == 0
}

// IsOnLastLine reports whether the byte offset cursor within value
// lies on the last visual line.
func IsOnLastLine(value string, cursor int) bool {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	return strings.IndexByte(value[cursor:], '\n') == -1
}

// InputCursorByteOffset returns the byte offset of the textarea
// cursor in m.Value().
func InputCursorByteOffset(m tas.Model) int {
	value := m.Value()
	line := m.Line()
	col := m.Column()

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
		return len(value)
	}

	rowEnd := len(value)
	if nl := strings.IndexByte(value[rowStart:], '\n'); nl >= 0 {
		rowEnd = rowStart + nl
	}

	rest := value[rowStart:rowEnd]
	byteOff := 0
	for runesWalked := 0; runesWalked < col && byteOff < len(rest); runesWalked++ {
		_, size := utf8.DecodeRuneInString(rest[byteOff:])
		byteOff += size
	}
	return rowStart + byteOff
}
