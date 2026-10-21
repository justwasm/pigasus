package main

// wordmove.go — Alt/Option+Left and Alt/Option+Right word-boundary
// cursor movement for the prompt editor.
//
// Cycle 225: the AGY 1.2.16 changelog ships "Added word-wise cursor
// movement in the prompt editor: Alt/Option+Left and Alt/Option+Right
// move the cursor to the previous/next word boundary, matching
// macOS text-field conventions." The reference binary treats the two
// as the macOS NSText / Cocoa convention:
//
//   - Alt+Right: from the cursor, skip any run of whitespace forward,
//     then skip the next run of non-whitespace. Land just past the
//     last non-whitespace character. At the end of the buffer the
//     cursor stays put.
//
//   - Alt+Left: from the cursor, skip any run of whitespace to the
//     left, then skip the next run of non-whitespace going left. Land
//     at the position of the first non-whitespace character in that
//     word. At the start of the buffer the cursor stays put.
//
// "Whitespace" here means unicode.IsSpace (matches both bubbles'
// wordRight / wordLeft helpers and Cocoa's WCWordBoundary). The
// newline is whitespace, so multi-line input navigates across line
// boundaries — verified against the test plan in
// wordmove_test.go.
//
// Why a host-side implementation when bubbles already exposes a
// WordForward / WordBackward keymap bound to "alt+right" / "alt+left"?
// bubbles' match is string-equality on Key.String(), so it only fires
// when the terminal encodes Option+Left as Code=KeyLeft + ModAlt. The
// legacy readline convention (ESC b / ESC f) and the kitty "leftalt"
// pseudo-code both reach the textarea as something other than
// "alt+left" and silently no-op. Doing the work here means the
// feature is guaranteed regardless of how the terminal encodes the
// key, and the unit tests can pin the exact macOS-convention positions
// down to the byte.
//
// The host handler in tui_run.go routes Alt+Left / Alt+Right through
// these helpers via runMode.moveWordLeft / runMode.moveWordRight; the
// vim-mode path keeps its existing single-character semantics on the
// unmodified key (vim Insert users get word movement via `b` / `w`,
// vim Normal users get it via `b` / `w`, and adding Alt+Left on top
// would conflict with their muscle memory).

import "unicode"

// wordBoundaryBack returns the rune-column the cursor should land on
// when the user presses Alt+Left from `col` on `s`. The semantics
// match macOS NSText Alt+Left (Cocoa WCWordBoundary, "previous-word"
// direction, skipWhitespace then skipNonWhitespace).
//
//   - If col <= 0 or col > len(s), returns 0 (no-op at start).
//   - Skips whitespace going left first (e.g. cursor at "hello | "
//     moves back over the trailing space to "hello |").
//   - Skips non-whitespace going left (e.g. "hello |" → "|hello "),
//     landing at the first non-whitespace of the prior word.
//   - At start-of-buffer, returns 0.
//
// The function operates on the line hosting the cursor. Callers that
// need cross-line motion (multi-line prompt) should concatenate the
// buffer first.
func wordBoundaryBack(s string, col int) int {
	runes := []rune(s)
	n := len(runes)
	if col <= 0 || n == 0 {
		return 0
	}
	if col > n {
		col = n
	}
	// Step 1: walk back over any whitespace directly to the left of
	// col. This puts us either at col 0, or on the first non-space
	// character to the left of the whitespace run.
	for col > 0 && unicode.IsSpace(runes[col-1]) {
		col--
	}
	if col <= 0 {
		return 0
	}
	// Step 2: walk back over the non-whitespace word we are now
	// inside (or just past the end of), landing at the first
	// non-whitespace rune of that word.
	for col > 0 && !unicode.IsSpace(runes[col-1]) {
		col--
	}
	return col
}

// wordBoundaryFwd returns the rune-column the cursor should land on
// when the user presses Alt+Right from `col` on `s`. The semantics
// match macOS NSText Alt+Right (Cocoa WCWordBoundary, "next-word"
// direction, skipWhitespace then skipNonWhitespace).
//
//   - If col >= len(s), returns col (no-op at end of buffer).
//   - Skips whitespace going forward (e.g. cursor at "| hello" moves
//     past the leading space to "|hello").
//   - Skips the next non-whitespace word, landing just past its last
//     character (e.g. "|hello" → "hello|").
//   - At end-of-buffer, returns len(s).
//
// The function operates on the line hosting the cursor. Callers that
// need cross-line motion (multi-line prompt) should concatenate the
// buffer first.
func wordBoundaryFwd(s string, col int) int {
	runes := []rune(s)
	n := len(runes)
	if col < 0 {
		col = 0
	}
	if col >= n {
		return n
	}
	// Step 1: skip whitespace forward. If we reach end-of-buffer,
	// land there (the next word is past the end).
	for col < n && unicode.IsSpace(runes[col]) {
		col++
	}
	if col >= n {
		return n
	}
	// Step 2: skip the non-whitespace word we are now at (or just
	// before). Land one past its last rune, which is the macOS
	// Alt+Right landing position (the end of the word).
	for col < n && !unicode.IsSpace(runes[col]) {
		col++
	}
	return col
}

// wordMoveLeft is the runMode-facing wrapper. It splits `value` on
// '\n' (bubbles textarea's line separator) and runs wordBoundaryBack
// on the joined buffer so multi-line input moves across line
// boundaries, then translates the absolute rune offset back to a
// (row, col) pair and applies it via m.input.SetCursorColumn (after
// walking rows with CursorUp / CursorDown).
//
// Returns the rune offset the cursor landed on, and a boolean
// reporting whether the position actually changed. A false return
// means the cursor was already at a boundary and the call is a no-op
// (matches the reference: Alt+Left at the start of the buffer is a
// no-op, not a "clamp and move to col 0" gesture).
//
// The function tolerates col beyond the buffer length (clamps it).
func wordMoveLeft(value string, row, col int) (newRow, newCol int, moved bool) {
	return wordMove(value, row, col, wordBoundaryBack)
}

// wordMoveRight is the Alt+Right counterpart.
func wordMoveRight(value string, row, col int) (newRow, newCol int, moved bool) {
	return wordMove(value, row, col, wordBoundaryFwd)
}

// wordMove is the shared helper. `step` is wordBoundaryBack or
// wordBoundaryFwd — both work on the joined buffer's rune space and
// return the absolute rune offset for the new cursor position.
//
// The absolute offset is mapped back to (row, col) by walking the
// '\n' separators in the joined buffer.
func wordMove(value string, row, col int, step func(string, int) int) (int, int, bool) {
	runes := []rune(value)
	n := len(runes)
	if n == 0 {
		return 0, 0, false
	}
	// Clamp row/col into range — the runMode can briefly hold stale
	// values during a slash rewrite.
	if row < 0 {
		row = 0
	}
	lines := splitLines(runes)
	if row >= len(lines) {
		row = len(lines) - 1
	}
	if col < 0 {
		col = 0
	}
	if col > len(lines[row]) {
		col = len(lines[row])
	}
	// Compute absolute rune offset for (row, col).
	abs := 0
	for i := 0; i < row; i++ {
		abs += len(lines[i]) + 1 // +1 for '\n'
	}
	abs += col
	if abs < 0 {
		abs = 0
	}
	if abs > n {
		abs = n
	}
	target := step(value, abs)
	if target == abs {
		return row, col, false
	}
	// Map target back to (row, col).
	newRow, newCol := 0, 0
	off := 0
	for i, l := range lines {
		ll := len(l)
		if target <= off+ll {
			newRow = i
			newCol = target - off
			return newRow, newCol, true
		}
		off += ll + 1
		newRow = i + 1
		newCol = 0
	}
	return newRow, newCol, true
}

// splitLines splits the textarea's rune buffer on '\n', returning one
// rune slice per visual line. A trailing '\n' produces a final empty
// line, matching bubbles' m.value layout (one rune slice per row, with
// the newline implicit).
func splitLines(runes []rune) [][]rune {
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
