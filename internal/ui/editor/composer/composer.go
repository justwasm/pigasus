// Package composer holds the composer's text-editing mechanics.
//
// The composer is the prompt's own input box: a textarea, the two
// dropdowns that hang off it (the slash-command popup and the "@"
// file-mention list) and the selection the user drags across it.
//
// This package holds the *mechanics* of editing that text: which key
// does what to a selection, the scan that removes a pasted-text
// placeholder as one unit, the backslash-newline fallback, and the
// update that keeps the dropdowns in step with the textarea.
//
// The mechanics are host-free. The host (internal/cliapp) owns the
// widget, the selection's store and the session's own facts (the
// prompt history, the eligibility notice, the mode hint); this
// package asks for them through a Seams struct and hands the results
// back as values.
//
// The value arithmetic a selection produces — where the cursor lands
// in a multi-line value, what a delete and a replace do to the value
// — belongs to the inputselection package, which is a sibling of this
// one; this package holds the *decisions*, not the arithmetic.
package composer

import (
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	"github.com/justwasm/pigasus/internal/ui/foundations/inputselection"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
)

// Range is a byte range in the composer's value.
//
// The range is normalized: Start is the lower offset and End the
// higher one, exactly as the host's selection store normalizes it. An
// empty range means no selection.
type Range struct {
	Start int
	End   int
}

// Empty reports whether the range selects nothing.
func (r Range) Empty() bool { return r.Start == r.End }

// Editor is the composer's editing widget: the subset of the textarea
// this package drives.
//
// The host owns the widget — this package never creates one — and
// hands it in through the Seams. A host that wants a different widget
// implements the interface itself.
type Editor interface {
	// Value is the widget's plain text.
	Value() string
	// SetValue replaces the widget's text.
	SetValue(s string)
	// Line and Column are the cursor's line and its rune column.
	Line() int
	Column() int
	// CursorOffset is the cursor's byte offset in Value.
	CursorOffset() int
	// InsertRune inserts one rune at the cursor.
	InsertRune(r rune)
	// Backspace removes the character before the cursor.
	Backspace()
	// Update feeds one message to the widget and answers the command
	// the widget produced.
	Update(msg tea.Msg) tea.Cmd
	// MoveCursorTo puts the cursor at the given line and column.
	MoveCursorTo(line, col int)
}

// TextArea is the Editor over the textarea widget the host owns.
//
// The widget stays the host's: this adapter writes through to it, so
// the host keeps reading the same field it always read.
type TextArea struct {
	m *tas.Model
}

// NewTextArea returns the Editor over the given textarea widget.
func NewTextArea(m *tas.Model) *TextArea { return &TextArea{m: m} }

// Value is the widget's plain text.
func (a *TextArea) Value() string { return a.m.Value() }

// SetValue replaces the widget's text.
func (a *TextArea) SetValue(s string) { a.m.SetValue(s) }

// Line is the cursor's line.
func (a *TextArea) Line() int { return a.m.Line() }

// Column is the cursor's rune column.
func (a *TextArea) Column() int { return a.m.Column() }

// CursorOffset is the cursor's byte offset in the value.
func (a *TextArea) CursorOffset() int { return tuilib.InputCursorByteOffset(*a.m) }

// InsertRune inserts one rune at the cursor.
func (a *TextArea) InsertRune(r rune) { a.m.InsertRune(r) }

// Backspace removes the character before the cursor.
func (a *TextArea) Backspace() {
	updated, _ := a.m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	*a.m = updated
}

// Update feeds one message to the widget and answers the command the
// widget produced.
func (a *TextArea) Update(msg tea.Msg) tea.Cmd {
	updated, cmd := a.m.Update(msg)
	*a.m = updated
	return cmd
}

// MoveCursorTo puts the cursor at the given line and column.
func (a *TextArea) MoveCursorTo(line, col int) {
	a.m.MoveToBegin()
	for i := 0; i < line; i++ {
		a.m.CursorDown()
	}
	a.m.SetCursorColumn(col)
}

// Seams are the host's facts the composer cannot know.
//
// The composer owns no state of its own: the widget, the selection's
// range, the prompt history and the session's notices all belong to
// the host, and this package asks for them here.
//
// The two dropdown panels are nil-safe — a panel that was never built
// answers "nothing to show" — and so is the history. The callbacks
// are the host's own effects and are required: a host that drives the
// composer provides them all.
type Seams struct {
	// Editor is the composer's editing widget.
	Editor Editor

	// Picker is the slash-command completion popup and Files is the
	// "@" file-mention dropdown. The composer keeps both in step
	// with the widget's value.
	Picker *slashpicker.Panel
	Files  *mentionpicker.Panel

	// History is the prompt history. The composer resets its
	// navigation when the text changes under a running navigation:
	// the user has typed something else, so the recalled prompt is no
	// longer the one the cursor is on.
	History *history.List

	// Selection is the composer's selection, as a byte range in the
	// editor's value.
	Selection Range

	// SelectAll selects the whole value, moves the cursor to its end
	// and copies it. It answers false when there is nothing to
	// select.
	SelectAll func() (tea.Cmd, bool)

	// ClearSelection drops the selection.
	ClearSelection func()

	// DeleteSelection removes the selected text and clears the
	// selection.
	DeleteSelection func()

	// ReplaceSelection replaces the selected text with s and clears
	// the selection.
	ReplaceSelection func(s string)

	// Copy writes text to the system clipboard and answers the
	// command that announces the copy.
	//
	// The write is OS I/O and stays on the host's side of this seam:
	// the composer decides *that* a selection is copied, the host
	// performs the write.
	Copy func(text string) tea.Cmd

	// ClearEligibilityNotice drops the session's eligibility notice.
	ClearEligibilityNotice func()

	// DismissModeHint dismisses the mode hint the composer shows
	// when the user starts typing.
	DismissModeHint func()

	// UndoMgr is the multi-level undo and redo manager. Optional (nil-safe).
	UndoMgr *UndoManager
}

// MoveCursor puts the cursor at the given byte offset in the value.
//
// The arithmetic is the inputselection package's — it says where the
// offset sits — and the editor moves there.
func (s Seams) MoveCursor(offset int) {
	line, col := inputselection.CursorPosition(s.Editor.Value(), offset)
	s.Editor.MoveCursorTo(line, col)
}

// afterEdit is the tail every text edit shares: the mode hint goes,
// the two dropdowns re-filter against the new value, the eligibility
// notice is cleared and the prompt history's navigation is reset.
func (s Seams) afterEdit() {
	s.DismissModeHint()
	s.Picker.Update([]rune(tuilib.FirstLine(s.Editor.Value())))
	s.ClearEligibilityNotice()
	s.Files.Update()
	if s.History != nil {
		s.History.Reset()
	}
}
