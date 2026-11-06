// undo.go — multi-level undo and redo history for the composer.
//
// Ports generic snapshot-based UndoStack and debounce grouping from Pi/PiG.
// Supports:
//  - Ctrl+Z / Cmd+Z: Undo
//  - Ctrl+Y / Cmd+Shift+Z: Redo
//  - Debounce grouping: consecutive keystrokes within debounce interval
//    are grouped into a single undo step.
//  - Distinct snapshots for whitespace, backspace, delete, paste, and selection replacements.

package composer

import (
	"time"
)

// DefaultDebounceDuration is the window within which consecutive typing
// is merged into a single undo step.
const DefaultDebounceDuration = 600 * time.Millisecond

// Snapshot records the editor text and cursor byte offset.
type Snapshot struct {
	Value        string
	CursorOffset int
}

// UndoStack stores snapshots with an optional capacity limit.
type UndoStack[T any] struct {
	stack []T
	limit int
}

// NewUndoStack creates an UndoStack with a maximum capacity.
func NewUndoStack[T any](limit int) *UndoStack[T] {
	if limit <= 0 {
		limit = 100
	}
	return &UndoStack[T]{limit: limit}
}

// Push appends an item to the stack, dropping oldest if limit exceeded.
func (u *UndoStack[T]) Push(item T) {
	if u.limit > 0 && len(u.stack) >= u.limit {
		u.stack = u.stack[1:]
	}
	u.stack = append(u.stack, item)
}

// Pop removes and returns the most recent item.
func (u *UndoStack[T]) Pop() (T, bool) {
	var zero T
	if len(u.stack) == 0 {
		return zero, false
	}
	lastIdx := len(u.stack) - 1
	item := u.stack[lastIdx]
	u.stack = u.stack[:lastIdx]
	return item, true
}

// Peek returns the top item without removing it.
func (u *UndoStack[T]) Peek() (T, bool) {
	var zero T
	if len(u.stack) == 0 {
		return zero, false
	}
	return u.stack[len(u.stack)-1], true
}

// Clear drops all items.
func (u *UndoStack[T]) Clear() {
	u.stack = nil
}

// Len returns the current count of snapshots.
func (u *UndoStack[T]) Len() int {
	return len(u.stack)
}

// UndoManager manages undo and redo history for the composer text editor.
type UndoManager struct {
	undoStack *UndoStack[Snapshot]
	redoStack *UndoStack[Snapshot]

	lastKind string
	lastTime time.Time
	debounce time.Duration
}

// NewUndoManager creates an initialized UndoManager.
func NewUndoManager(capacity int) *UndoManager {
	return &UndoManager{
		undoStack: NewUndoStack[Snapshot](capacity),
		redoStack: NewUndoStack[Snapshot](capacity),
		debounce:  DefaultDebounceDuration,
	}
}

// RecordBeforeChange records an editor snapshot before a change occurs.
// If kind is "type", consecutive typing within the debounce window is merged.
func (m *UndoManager) RecordBeforeChange(val string, cursor int, kind string) {
	now := time.Now()

	// If this is continuous typing and not whitespace, merge within debounce interval.
	if kind == "type" && m.lastKind == "type" && now.Sub(m.lastTime) < m.debounce {
		m.lastTime = now
		return
	}

	// Avoid pushing duplicate snapshot of identical text and cursor.
	if top, ok := m.undoStack.Peek(); ok && top.Value == val && top.CursorOffset == cursor {
		m.lastKind = kind
		m.lastTime = now
		return
	}

	m.undoStack.Push(Snapshot{Value: val, CursorOffset: cursor})
	m.redoStack.Clear()
	m.lastKind = kind
	m.lastTime = now
}

// RecordType records typing a character or string. Whitespace forces a boundary.
func (m *UndoManager) RecordType(val string, cursor int, char string) {
	if char == " " || char == "\t" || char == "\n" {
		m.RecordBeforeChange(val, cursor, "whitespace")
	} else {
		m.RecordBeforeChange(val, cursor, "type")
	}
}

// Undo pops the last snapshot from the undo stack and saves current state to redo stack.
func (m *UndoManager) Undo(currentVal string, currentCursor int) (Snapshot, bool) {
	snap, ok := m.undoStack.Pop()
	if !ok {
		return Snapshot{}, false
	}
	m.redoStack.Push(Snapshot{Value: currentVal, CursorOffset: currentCursor})
	m.lastKind = ""
	return snap, true
}

// Redo pops the last snapshot from the redo stack and saves current state to undo stack.
func (m *UndoManager) Redo(currentVal string, currentCursor int) (Snapshot, bool) {
	snap, ok := m.redoStack.Pop()
	if !ok {
		return Snapshot{}, false
	}
	m.undoStack.Push(Snapshot{Value: currentVal, CursorOffset: currentCursor})
	m.lastKind = ""
	return snap, true
}

// CanUndo reports whether an undo snapshot is available.
func (m *UndoManager) CanUndo() bool {
	return m.undoStack.Len() > 0
}

// CanRedo reports whether a redo snapshot is available.
func (m *UndoManager) CanRedo() bool {
	return m.redoStack.Len() > 0
}

// Clear clears both undo and redo stacks.
func (m *UndoManager) Clear() {
	m.undoStack.Clear()
	m.redoStack.Clear()
	m.lastKind = ""
}
