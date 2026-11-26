// mouse.go — the Vim half of the composer's mouse selection.
//
// agy 1.5.17: "When Vim mode is on, mouse selections outside Insert mode
// enter characterwise Visual mode so Vim operators and motions act on
// the selection, and Normal-mode clicks past the end of a line land on
// the last character."
//
// So a press in the composer has to do two things at once when the
// engine is not in Insert mode: put the engine's cursor where the
// pointer is, and turn whatever the pointer highlighted into the
// engine's own Visual selection — otherwise the operator that follows
// ("d", "y", "c", "~") would act on a phantom caret at column 0 while
// the screen showed a highlighted range.
//
// Everything here goes through the engine's public surface only
// (State.Row/Col/Mode and State.Handle): the Visual anchor is
// unexported, so it is set the way a user sets it — put the cursor at
// the anchor and press "v". The engine therefore stays the one state
// machine that owns the mode, the anchor and the clamp rule, and the
// bridge holds no copy of them.
package vimbridge

import (
	"unicode/utf8"

	"github.com/justwasm/pigasus/internal/ui/foundations/inputselection"
	"github.com/justwasm/pigasus/vim"
)

// MousePending reports whether the engine is a candidate for the
// Vim-side of a composer mouse selection: vim is on and the engine is
// not in Insert mode.
//
// Insert mode is excluded on purpose. There the textarea's own caret is
// the cursor and the composer's plain selection already does the right
// thing (type to replace, Backspace to delete, Ctrl+C to copy), and
// handing the range to the engine would leave Insert mode — which is
// exactly what the clause says must not happen.
func (b *Bridge) MousePending() bool {
	return b != nil && b.Enabled() && b.Engine().Mode != vim.Insert
}

// MouseClick places the engine's cursor at a byte offset.
//
// Normal-mode clicks past the end of a line land on the last character:
// agy 1.5.17 names that case separately because a Normal/Visual cursor
// must sit *on* a character (Insert may rest one past the last one). A
// naive "set Column to the click's rune column" leaves the caret one
// past the text, i.e. on a phantom position. normalCol applies the
// engine's own rule — the one State.clampCursor enforces — so a click
// past end-of-line lands on the last character and an empty line keeps
// column 0.
//
// It answers false when vim is off or the engine was never built.
func (b *Bridge) MouseClick(offset int) bool {
	if b == nil || !b.Enabled() {
		return false
	}
	eng := b.Engine()
	if eng == nil {
		return false
	}
	// A press leaves Visual: the previous highlight is gone by the time
	// the click lands, and the caret is what the user asked for.
	if eng.Mode == vim.Visual || eng.Mode == vim.VisualLine {
		eng.Handle(vim.Key{Name: "esc"})
	}
	// A press snaps back to a rune boundary first: a byte offset in the
	// middle of a multi-byte rune (a caller may hand in any offset) is not
	// a cursor position, and the rune it lands in is the one the user
	// pointed at.
	offset = snapToRuneStart(eng.Text(), offset)
	line, col := inputselection.CursorPosition(eng.Text(), offset)
	if line >= len(eng.Lines) {
		line = len(eng.Lines) - 1
	}
	if line < 0 {
		line = 0
	}
	eng.Row = line
	eng.Col = normalCol(eng.Lines[line], col)
	b.SyncInput()
	return true
}

// SelectRange hands a [start, end) byte range in the composer to the
// engine as a characterwise Visual selection.
//
// The engine's anchor is the range's start and its cursor the range's
// last character (the range is exclusive, Visual is inclusive), which is
// how a mouse drag reads: the anchor where the press landed, the caret
// where the pointer is.
//
// It answers false when vim is off, the engine is in Insert mode, or the
// range covers nothing — the caller then keeps the composer's own
// selection.
func (b *Bridge) SelectRange(start, end int) bool {
	if b == nil || !b.MousePending() {
		return false
	}
	eng := b.Engine()
	if eng == nil {
		return false
	}
	if start > end {
		start, end = end, start
	}
	text := eng.Text()
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	if start >= end {
		return false
	}
	startLine, startCol := inputselection.CursorPosition(text, start)
	// The end offset is exclusive; Visual's caret sits on the last
	// selected character.
	last := lastRuneStart(text, start, end)
	endLine, endCol := inputselection.CursorPosition(text, last)
	if startLine >= len(eng.Lines) || endLine >= len(eng.Lines) {
		return false
	}

	// Put the caret on the anchor and enter Visual, so the engine's own
	// entry point records the anchor. A stale Visual is left first.
	if eng.Mode == vim.Visual || eng.Mode == vim.VisualLine {
		eng.Handle(vim.Key{Name: "esc"})
	}
	eng.Row = startLine
	eng.Col = normalCol(eng.Lines[startLine], startCol)
	eng.Handle(vim.Key{Rune: 'v'})
	if eng.Mode != vim.Visual {
		return false
	}
	eng.Row = endLine
	eng.Col = normalCol(eng.Lines[endLine], endCol)
	b.SyncInput()
	return true
}

// ClearMouseSelection drops a Visual selection the mouse put the engine
// into, returning it to Normal. It answers whether it changed anything.
//
// It is what a press elsewhere uses: the engine's mode is its own, so the
// host asks for the way back through the engine's Esc rather than
// writing the field.
func (b *Bridge) ClearMouseSelection() bool {
	if b == nil || !b.Enabled() {
		return false
	}
	eng := b.Engine()
	if eng == nil || (eng.Mode != vim.Visual && eng.Mode != vim.VisualLine) {
		return false
	}
	eng.Handle(vim.Key{Name: "esc"})
	b.SyncInput()
	return true
}

// normalCol applies the engine's Normal/Visual column rule to col on
// line: the caret must sit on a character, so it stops at runeLen-1, and
// an empty line keeps column 0. Insert mode's "one past the end" is not
// this rule — that is why the bridge asks the mode first.
func normalCol(line string, col int) int {
	n := utf8.RuneCountInString(line)
	if n == 0 || col < 0 {
		return 0
	}
	if col >= n {
		return n - 1
	}
	return col
}

// lastRuneStart returns the byte offset of the rune that ends the range
// [start, end): the offset of the last rune wholly inside it, or start
// when the range holds no complete rune.
//
// A newline inside the range is a rune like any other: a characterwise
// Visual range that crosses a line break must keep the caret on the
// last character of the *last* line it reaches, which is what makes the
// multi-line "d" a characterwise delete rather than a line-wise one.
func lastRuneStart(text string, start, end int) int {
	last := start
	for i := start; i < end; {
		_, size := utf8.DecodeRuneInString(text[i:])
		if size <= 0 || i+size > end {
			break
		}
		last = i
		i += size
	}
	return last
}

// snapToRuneStart backs offset up to the start of the rune it falls
// inside, so a byte offset mid-character reads as the character the user
// pointed at rather than as a partial rune.
func snapToRuneStart(text string, offset int) int {
	if offset <= 0 {
		return 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	if offset == len(text) || utf8.RuneStart(text[offset]) {
		return offset
	}
	for offset > 0 && !utf8.RuneStart(text[offset]) {
		offset--
	}
	return offset
}
