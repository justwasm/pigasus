// Package textbuffer holds the small text buffer the interactive panels
// type into: the inline comment editor of the file viewer, the search
// prompt of the same viewer.
//
// # What it is
//
// The buffer is a value: the panel owns it, resets it when the input
// opens and reads the text back when the input closes. The buffer never
// reads the keyboard on its own — the panel hands it the key and asks
// what it made of it — so the *decision* of what a key means stays with
// the panel.
//
// Enter is not the buffer's key: the panel decides what Enter means (it
// saves the comment, it runs the search). Alt+Enter and Shift+Enter are
// the buffer's newline, the reference's own rule for a multi-line note
// (official-docs/cli/commands/codesearch.md:82).
//
// # Where it lives
//
// The buffer is shared machinery, so it lives below the panels that use
// it: the /codesearch viewer (internal/ui/views/codesearchpanel) types
// into one today, and any other viewer that carries an inline input
// answers through the same one.
package textbuffer

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The cursor's own span.
//
// The palette has no export for a reverse-video cell, so the package
// declares the span itself — the same precedent as the other ui-tier
// rows (the notice rows declare their own close code).
const (
	// cursorOpen marks the cursor's cell in reverse video.
	cursorOpen = "\x1b[7m"
	// cursorClose closes the reverse-video span.
	cursorClose = "\x1b[27m"
)

// Buffer is a multi-line text buffer with a cursor.
//
// The zero value is usable: it holds one empty line.
type Buffer struct {
	// lines are the buffer's lines, one entry per line.
	lines [][]rune
	// row and col are the cursor's position: the line and the column
	// inside it, both counted from zero.
	row, col int
}

// New returns an empty buffer.
func New() Buffer {
	return Buffer{lines: [][]rune{{}}}
}

// Reset empties the buffer and puts the cursor at the start.
func (b *Buffer) Reset() {
	if b == nil {
		return
	}
	b.lines = [][]rune{{}}
	b.row, b.col = 0, 0
}

// Value is the buffer's text: the lines joined with a newline.
func (b *Buffer) Value() string {
	if b == nil {
		return ""
	}
	parts := make([]string, len(b.lines))
	for i, l := range b.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

// SetValue replaces the buffer's text with the given one.
//
// The text's newlines split it into lines; the cursor goes to the end of
// the last one.
func (b *Buffer) SetValue(text string) {
	if b == nil {
		return
	}
	b.lines = nil
	for _, l := range strings.Split(text, "\n") {
		b.lines = append(b.lines, []rune(l))
	}
	if len(b.lines) == 0 {
		b.lines = [][]rune{{}}
	}
	b.row = len(b.lines) - 1
	b.col = len(b.lines[b.row])
}

// HandleKey applies one key to the buffer.
//
// It reports whether the key was the buffer's. Enter is not: the panel
// decides what Enter means, and the buffer only claims the modified
// Enter — the newline.
func (b *Buffer) HandleKey(key tea.Key) bool {
	if b == nil {
		return false
	}
	switch key.Code {
	case tea.KeyEnter:
		if key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModShift) {
			b.newline()
			return true
		}
		return false
	case tea.KeyBackspace:
		b.backspace()
		return true
	case tea.KeyDelete:
		b.delete()
		return true
	case tea.KeyLeft:
		b.left()
		return true
	case tea.KeyRight:
		b.right()
		return true
	case tea.KeyUp:
		b.up()
		return true
	case tea.KeyDown:
		b.down()
		return true
	case tea.KeyHome:
		b.col = 0
		return true
	case tea.KeyEnd:
		b.col = len(b.lines[b.row])
		return true
	}
	// A printable rune. A key with a control or an alt modifier is a
	// command, not text.
	if key.Text == "" || key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModAlt) {
		return false
	}
	for _, r := range key.Text {
		b.insert(r)
	}
	return true
}

// Rows renders the buffer's lines, each one behind the prefix, with the
// cursor marked.
//
// The prefix is the panel's: the prompt that says what the input is for.
func (b *Buffer) Rows(prefix string) []string {
	if b == nil {
		return nil
	}
	rows := make([]string, 0, len(b.lines))
	for i, line := range b.lines {
		text := string(line)
		if i == b.row {
			text = markCursor(text, b.col)
		}
		rows = append(rows, prefix+text)
	}
	return rows
}

// markCursor marks the cursor's cell in reverse video.
//
// A cursor at the end of the line marks a space, so the cursor is
// always visible.
func markCursor(text string, col int) string {
	r := []rune(text)
	if col >= len(r) {
		return text + cursorOpen + " " + cursorClose
	}
	return string(r[:col]) + cursorOpen + string(r[col]) + cursorClose + string(r[col+1:])
}

// insert writes a rune at the cursor.
func (b *Buffer) insert(r rune) {
	line := b.lines[b.row]
	if b.col > len(line) {
		b.col = len(line)
	}
	next := make([]rune, 0, len(line)+1)
	next = append(next, line[:b.col]...)
	next = append(next, r)
	next = append(next, line[b.col:]...)
	b.lines[b.row] = next
	b.col++
}

// backspace deletes the rune before the cursor.
func (b *Buffer) backspace() {
	if b.col == 0 {
		return
	}
	line := b.lines[b.row]
	b.lines[b.row] = append(line[:b.col-1:b.col-1], line[b.col:]...)
	b.col--
}

// delete deletes the rune at the cursor.
func (b *Buffer) delete() {
	line := b.lines[b.row]
	if b.col >= len(line) {
		return
	}
	b.lines[b.row] = append(line[:b.col:b.col], line[b.col+1:]...)
}

// newline splits the cursor's line in two.
func (b *Buffer) newline() {
	line := b.lines[b.row]
	if b.col > len(line) {
		b.col = len(line)
	}
	head := append([]rune{}, line[:b.col]...)
	tail := append([]rune{}, line[b.col:]...)
	b.lines[b.row] = head
	rest := make([][]rune, 0, len(b.lines))
	rest = append(rest, b.lines[:b.row+1]...)
	rest = append(rest, tail)
	rest = append(rest, b.lines[b.row+1:]...)
	b.lines = rest
	b.row++
	b.col = 0
}

// left moves the cursor one cell to the left.
func (b *Buffer) left() {
	if b.col > 0 {
		b.col--
		return
	}
	if b.row > 0 {
		b.row--
		b.col = len(b.lines[b.row])
	}
}

// right moves the cursor one cell to the right.
func (b *Buffer) right() {
	if b.col < len(b.lines[b.row]) {
		b.col++
		return
	}
	if b.row < len(b.lines)-1 {
		b.row++
		b.col = 0
	}
}

// up moves the cursor to the line above.
func (b *Buffer) up() {
	if b.row == 0 {
		return
	}
	b.row--
	b.clampCol()
}

// down moves the cursor to the line below.
func (b *Buffer) down() {
	if b.row >= len(b.lines)-1 {
		return
	}
	b.row++
	b.clampCol()
}

// clampCol keeps the cursor's column inside its line.
func (b *Buffer) clampCol() {
	if b.col > len(b.lines[b.row]) {
		b.col = len(b.lines[b.row])
	}
}
