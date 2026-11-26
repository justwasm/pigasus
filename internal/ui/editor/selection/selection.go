// Package selection owns the composer's input-selection state and transitions.
//
// It is deliberately independent of Bubble Tea and RootModel. A host supplies
// only the editor port needed to apply edits and move the cursor; clipboard
// writes remain a host effect, returned as selected text from SelectAll/End.
// Geometry and ANSI highlighting are delegated to the sibling foundations
// package, so this module can be constructed and exercised on its own.
package selection

import (
	"github.com/justwasm/pigasus/internal/ui/foundations/inputselection"
)

// DoubleClickWindowNanos is how long two presses on the same byte offset
// stay a double-click, in nanoseconds (500ms).
//
// The bubbletea fork's Mouse carries no click count (checked: mouse.go in
// github.com/bubbletui/bubbletea/v2@v2.1.1 declares only X, Y, Button and
// Mod), and the terminal reports presses, so the count is the host's to
// derive. 500ms is the platform convention (X11's default double-click
// time, and what macOS and Windows use unless the user changes it).
//
// The clock is a plain int64 rather than a time.Time because this module
// is deliberately free of "time" (internal/core/architecture's boundary
// rule for internal/ui/editor/selection): the host reads its clock and
// hands the value in, so the count stays a pure function of two numbers.
const DoubleClickWindowNanos int64 = 500 * 1000 * 1000

// Editor is the narrow textarea port used by selection edits.
type Editor interface {
	Value() string
	SetValue(string)
	MoveCursorTo(line, col int)
}

// CopyRequest asks the host to write Text to its clipboard and emit the
// terminal clipboard command. The module never performs platform I/O.
type CopyRequest struct {
	Text string
}

// Model owns the composer's selection range and drag lifecycle.
// Its zero value is ready for use.
type Model struct {
	dragging bool
	start    int
	end      int

	// lastClickAt / lastClick / clickCount are the click tracker: the
	// bubbletea fork's Mouse carries no click count, so a repeated
	// press on the same offset inside DoubleClickWindowNanos is what
	// makes a double-click. clickCount is 2 for the second press and 1
	// otherwise; there is no triple-click behaviour to count for.
	lastClickAt int64
	lastClick   int
	clickCount  int

	// wordDragging is set by BeginWord: the drag then extends a whole
	// word at a time and its endpoint is never the raw pointer offset,
	// so End must not overwrite it.
	wordDragging bool
	wordStart    int
	wordEnd      int
}

// ResetInputSelection clears the active range and drag state.
func (m *Model) ResetInputSelection() {
	m.dragging = false
	m.start = 0
	m.end = 0
	m.wordDragging = false
	m.wordStart = 0
	m.wordEnd = 0
}

// ClickAt records a press at offset at nowNanos (a monotonic clock
// reading in nanoseconds) and answers the click count: 2 when the
// previous press was on the same offset within DoubleClickWindowNanos,
// 1 otherwise.
//
// The clock is an argument so the count is a pure function of the two
// presses and a test can drive it without sleeping, and so this module
// needs no clock of its own.
func (m *Model) ClickAt(offset int, nowNanos int64) int {
	if m.clickCount != 0 && m.lastClick == offset &&
		nowNanos >= m.lastClickAt && nowNanos-m.lastClickAt < DoubleClickWindowNanos {
		m.clickCount = 2
	} else {
		m.clickCount = 1
	}
	m.lastClick = offset
	m.lastClickAt = nowNanos
	return m.clickCount
}

// ClickCount is the count the last ClickAt produced (0 before any press).
func (m *Model) ClickCount() int { return m.clickCount }

// ResetClicks forgets the click tracker, so the next press starts a
// fresh count. A press that is not on the composer (a click in the
// transcript, a key that moved the caret) is not the first half of a
// double-click.
func (m *Model) ResetClicks() {
	m.lastClickAt = 0
	m.lastClick = 0
	m.clickCount = 0
}

// WordDragging reports whether the active drag extends by whole words.
func (m *Model) WordDragging() bool { return m.wordDragging }

// InputSelectionRange returns the normalized byte range [start, end).
func (m *Model) InputSelectionRange() (start, end int) {
	if m.start <= m.end {
		return m.start, m.end
	}
	return m.end, m.start
}

// HasInputSelection reports whether a non-empty range is active.
func (m *Model) HasInputSelection() bool {
	return m.start != m.end
}

// InputSelectionDragging reports whether a mouse drag is in progress.
func (m *Model) InputSelectionDragging() bool { return m.dragging }

// SetRange selects a byte range supplied by a programmatic selection action.
// It does not begin a mouse drag; backwards ranges are normalized on read.
func (m *Model) SetRange(start, end int) {
	m.dragging = false
	m.start = start
	m.end = end
}

// Begin starts a drag at offset and moves the editor cursor there.
func (m *Model) Begin(offset int, editor Editor) {
	m.dragging = true
	m.wordDragging = false
	m.start = offset
	m.end = offset
	moveCursor(editor, offset)
}

// BeginWord starts a word drag: the word at offset is selected whole,
// and the drag that follows extends it a word at a time.
//
// The cursor lands at the start of the word, which is where a word
// drag's anchor sits — the same place Vim's own `v iw` leaves it.
func (m *Model) BeginWord(value string, offset int, editor Editor) {
	start, end := WordBounds(value, offset)
	if start == end {
		// No word under the pointer (a gap, a line break, an empty
		// composer): the press is an ordinary drag, so a following
		// drag still selects.
		m.Begin(offset, editor)
		return
	}
	m.dragging = true
	m.wordDragging = true
	m.wordStart = start
	m.wordEnd = end
	m.start = start
	m.end = end
	moveCursor(editor, start)
}

// Drag updates the end of an active drag and reports whether it moved.
//
// A word drag takes the whole word at offset on the side the pointer
// moved to: dragging right past the first word's end grows the range to
// the end of the word under the pointer, dragging left of the anchor
// grows it back to that word's start. The anchor word is always kept in
// the range, which is what makes a double-click-drag extend by words
// rather than jump.
func (m *Model) Drag(offset int, editor Editor) bool {
	if !m.dragging {
		return false
	}
	if m.wordDragging {
		return m.dragWord(offset, editor)
	}
	if m.end == offset {
		return false
	}
	m.end = offset
	moveCursor(editor, offset)
	return true
}

// dragWord extends a word drag to the word the pointer is over.
//
// The rule is one sentence: the range is the anchor word plus every
// whole word between it and the pointer's word. So the pointer's word
// decides the far edge — the anchor word's start with the pointer's
// word's end when the pointer is at or after the anchor, the pointer's
// word's start with the anchor word's end when it is before it, and the
// anchor word alone when the pointer is back inside it. That is what
// makes a double-click-drag extend by words rather than jump, and what
// lets dragging back shrink it again.
func (m *Model) dragWord(offset int, editor Editor) bool {
	value := editor.Value()
	start, end := WordBounds(value, offset)
	if start == end {
		// The pointer is in a gap between words. The gap belongs to no
		// word, so the range only moves when the pointer has left it:
		// past the range's end the next word forward is taken, before
		// the range's start the previous word backward. Otherwise the
		// range stands, which is what keeps the anchor word when the
		// pointer is merely between it and its neighbour.
		switch {
		case offset > m.end:
			start, end = WordBounds(value, offset+1)
		case offset < m.start:
			start, end = WordBounds(value, offset-1)
		default:
			return false
		}
		if start == end {
			return false
		}
	}
	newStart, newEnd := m.wordStart, m.wordEnd
	switch {
	case start < m.wordStart:
		// The pointer's word is wholly before the anchor word: the
		// range runs from its start to the anchor word's end.
		newStart, newEnd = start, m.wordEnd
	case end > m.wordEnd:
		// The pointer's word is wholly after the anchor word: the
		// range runs from the anchor word's start to its end.
		newStart, newEnd = m.wordStart, end
	}
	if newStart == m.start && newEnd == m.end {
		return false
	}
	m.start, m.end = newStart, newEnd
	moveCursor(editor, offset)
	return true
}

func moveCursor(editor Editor, offset int) {
	line, col := inputselection.CursorPosition(editor.Value(), offset)
	editor.MoveCursorTo(line, col)
}

// End finishes a drag. A left release uses offset as its endpoint; other
// buttons keep the last drag point.
//
// It never writes the clipboard and answers no copy request. agy
// 1.5.17: a drag and a double-click-drag "highlight text for typing or
// paste replacement, `Ctrl+C` copy, and `Ctrl+X` cut **without
// overwriting your clipboard on release**". The copy is `Ctrl+C`'s
// (Copy) and the cut is `Ctrl+X`'s (Cut); the release itself must leave
// whatever the user already had on the clipboard alone, so a mere drag
// cannot destroy it. This is the sub-behaviour the release path used to
// get wrong.
//
// A word drag keeps the range DragWord built — the release offset is
// the raw pointer position and would land mid-word — and so does a
// stale range (one whose ends no longer fit the value). Both keep the
// highlight. An empty range — a click without movement — is reset.
//
// It answers whether a highlight is still active.
func (m *Model) End(value string, offset int, leftButton bool) bool {
	m.dragging = false
	if m.wordDragging {
		m.wordDragging = false
		// The word drag's range stands as its last DragWord left it;
		// the release point is not a selection endpoint.
		start, end := m.InputSelectionRange()
		if start < end && end <= len(value) {
			return true
		}
		m.ResetInputSelection()
		return false
	}
	if leftButton {
		m.end = offset
	}
	start, end := m.InputSelectionRange()
	if start < end && start >= 0 && end <= len(value) {
		return true
	}
	if start == end {
		m.ResetInputSelection()
	}
	return false
}

// selectedText returns the bytes in [start, end) of value, or "" when
// the range does not fit.
func selectedText(value string, start, end int) string {
	if start < 0 || end > len(value) || start >= end {
		return ""
	}
	return value[start:end]
}

// Copy returns the text the current range covers, for the host's
// clipboard write. It answers false when no range is active or the
// range no longer fits the value — a stale range copies nothing rather
// than unrelated bytes.
func (m *Model) Copy(value string) (CopyRequest, bool) {
	if !m.HasInputSelection() {
		return CopyRequest{}, false
	}
	start, end := m.InputSelectionRange()
	text := selectedText(value, start, end)
	if text == "" {
		return CopyRequest{}, false
	}
	return CopyRequest{Text: text}, true
}

// Cut removes the current range, returns the text the host must place on
// the clipboard, and clears the selection. It answers false — and
// deletes nothing — when no range is active.
//
// This is the only place a selection's text reaches the clipboard by
// deletion. Doing the clipboard write *here*, at the moment of the cut,
// is what keeps the guarantee that a later mouse release cannot
// overwrite it: by the time the release arrives the range is gone, and
// End writes nothing at all.
func (m *Model) Cut(editor Editor) (CopyRequest, bool) {
	if !m.HasInputSelection() {
		return CopyRequest{}, false
	}
	start, end := m.InputSelectionRange()
	value := editor.Value()
	if start < 0 || end > len(value) || start >= end {
		m.ResetInputSelection()
		return CopyRequest{}, false
	}
	text := value[start:end]
	m.DeleteInputSelection(editor)
	return CopyRequest{Text: text}, true
}

// SelectAll selects the editor's complete value, moves its cursor to the end,
// and returns the selected text for the host's clipboard effect.
func (m *Model) SelectAll(editor Editor) (CopyRequest, bool) {
	value := editor.Value()
	if value == "" {
		return CopyRequest{}, false
	}
	m.start = 0
	m.end = len(value)
	line, col := inputselection.CursorPosition(value, len(value))
	editor.MoveCursorTo(line, col)
	return CopyRequest{Text: value}, true
}

// DeleteInputSelection removes the current range and clears it.
func (m *Model) DeleteInputSelection(editor Editor) {
	if !m.HasInputSelection() {
		return
	}
	start, end := m.InputSelectionRange()
	value := editor.Value()
	if start >= 0 && end <= len(value) {
		updated, _ := inputselection.DeleteRange(value, start, end)
		editor.SetValue(updated)
		line, col := inputselection.CursorPosition(updated, start)
		editor.MoveCursorTo(line, col)
	}
	m.ResetInputSelection()
}

// ReplaceInputSelection replaces the current range and clears it.
func (m *Model) ReplaceInputSelection(editor Editor, replacement string) {
	if !m.HasInputSelection() {
		return
	}
	start, end := m.InputSelectionRange()
	value := editor.Value()
	if start >= 0 && end <= len(value) {
		updated, _ := inputselection.ReplaceRange(value, start, end, replacement)
		editor.SetValue(updated)
		line, col := inputselection.CursorPosition(updated, start+len(replacement))
		editor.MoveCursorTo(line, col)
	}
	m.ResetInputSelection()
}

// ApplyHighlight paints the current range in the rendered editor view.
func (m *Model) ApplyHighlight(inputView, value string) string {
	start, end := m.InputSelectionRange()
	if start < 0 {
		return inputView
	}
	return inputselection.ApplyHighlight(inputView, value, start, end)
}
