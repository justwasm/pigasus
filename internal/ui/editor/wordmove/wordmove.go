// Package wordmove implements Alt/Option+Left and Alt/Option+Right word-boundary
// cursor movement for text editing following macOS Cocoa/NSText conventions.
package wordmove

import "unicode"

// BoundaryBack returns the rune-column the cursor should land on when pressing
// Alt+Left from col on s (macOS NSText Alt+Left: skip whitespace left, then
// skip non-whitespace left, landing at the first non-whitespace character).
func BoundaryBack(s string, col int) int {
	runes := []rune(s)
	n := len(runes)
	if col <= 0 || n == 0 {
		return 0
	}
	if col > n {
		col = n
	}
	for col > 0 && unicode.IsSpace(runes[col-1]) {
		col--
	}
	if col <= 0 {
		return 0
	}
	for col > 0 && !unicode.IsSpace(runes[col-1]) {
		col--
	}
	return col
}

// BoundaryFwd returns the rune-column the cursor should land on when pressing
// Alt+Right from col on s (macOS NSText Alt+Right: skip whitespace forward, then
// skip non-whitespace forward, landing just past the last non-whitespace character).
func BoundaryFwd(s string, col int) int {
	runes := []rune(s)
	n := len(runes)
	if col < 0 {
		col = 0
	}
	if col >= n {
		return n
	}
	for col < n && unicode.IsSpace(runes[col]) {
		col++
	}
	if col >= n {
		return n
	}
	for col < n && !unicode.IsSpace(runes[col]) {
		col++
	}
	return col
}

// MoveLeft splits value on '\n' and runs BoundaryBack across line boundaries.
func MoveLeft(value string, row, col int) (newRow, newCol int, moved bool) {
	return Move(value, row, col, BoundaryBack)
}

// MoveRight splits value on '\n' and runs BoundaryFwd across line boundaries.
func MoveRight(value string, row, col int) (newRow, newCol int, moved bool) {
	return Move(value, row, col, BoundaryFwd)
}

// Move calculates new row and column after applying step to the absolute rune offset.
func Move(value string, row, col int, step func(string, int) int) (int, int, bool) {
	runes := []rune(value)
	if len(runes) == 0 {
		return 0, 0, false
	}
	lines := SplitLines(runes)
	abs := toAbsoluteOffset(lines, row, col, len(runes))
	target := step(value, abs)
	if target == abs {
		return row, col, false
	}
	newRow, newCol := fromAbsoluteOffset(lines, target)
	return newRow, newCol, true
}

func toAbsoluteOffset(lines [][]rune, row, col, totalRunes int) int {
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	if col < 0 {
		col = 0
	}
	if col > len(lines[row]) {
		col = len(lines[row])
	}
	abs := 0
	for i := 0; i < row; i++ {
		abs += len(lines[i]) + 1
	}
	abs += col
	if abs < 0 {
		return 0
	}
	if abs > totalRunes {
		return totalRunes
	}
	return abs
}

func fromAbsoluteOffset(lines [][]rune, target int) (int, int) {
	off := 0
	for i, l := range lines {
		ll := len(l)
		if target <= off+ll {
			return i, target - off
		}
		off += ll + 1
	}
	return len(lines) - 1, 0
}

// SplitLines splits runes on '\n', returning one rune slice per visual line.
func SplitLines(runes []rune) [][]rune {
	var out [][]rune
	start := 0
	for i, r := range runes {
		if r == '\n' {
			out = append(out, runes[start:i])
			start = i + 1
		}
	}
	out = append(out, runes[start:])
	return out
}
