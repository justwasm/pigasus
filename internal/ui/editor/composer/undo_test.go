package composer

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestUndoStack(t *testing.T) {
	s := NewUndoStack[int](3)
	if s.Len() != 0 {
		t.Fatalf("expected 0, got %d", s.Len())
	}
	s.Push(1)
	s.Push(2)
	s.Push(3)
	s.Push(4) // should drop 1 due to limit 3

	if s.Len() != 3 {
		t.Fatalf("expected 3, got %d", s.Len())
	}

	val, ok := s.Pop()
	if !ok || val != 4 {
		t.Fatalf("expected 4, got %d", val)
	}

	val, ok = s.Peek()
	if !ok || val != 3 {
		t.Fatalf("expected peek 3, got %d", val)
	}

	s.Clear()
	if s.Len() != 0 {
		t.Fatalf("expected 0 after clear, got %d", s.Len())
	}
	_, ok = s.Pop()
	if ok {
		t.Fatalf("expected false on empty pop")
	}
}

func TestUndoRedo(t *testing.T) {
	mgr := NewUndoManager(50)
	mgr.debounce = 50 * time.Millisecond

	// 1. Initial typing: "hello"
	mgr.RecordType("", 0, "h")
	mgr.RecordType("h", 1, "e")
	mgr.RecordType("he", 2, "l")
	mgr.RecordType("hel", 3, "l")
	mgr.RecordType("hell", 4, "o")

	// Debounce grouped all 5 keystrokes into one initial snapshot: ""
	if mgr.undoStack.Len() != 1 {
		t.Fatalf("expected 1 undo snapshot due to debounce, got %d", mgr.undoStack.Len())
	}

	// 2. Typing space creates a new boundary
	mgr.RecordType("hello", 5, " ")

	// Now there are 2 snapshots: "" and "hello"
	if mgr.undoStack.Len() != 2 {
		t.Fatalf("expected 2 undo snapshots after space, got %d", mgr.undoStack.Len())
	}

	// 3. Type "world"
	mgr.RecordType("hello ", 6, "w")
	mgr.RecordType("hello w", 7, "o")

	// 3 snapshots: "", "hello", and "hello "
	if mgr.undoStack.Len() != 3 {
		t.Fatalf("expected 3 undo snapshots, got %d", mgr.undoStack.Len())
	}

	// Current text is "hello wo" (cursor 8)
	// Undo 1: should restore to "hello "
	snap, ok := mgr.Undo("hello wo", 8)
	if !ok || snap.Value != "hello " || snap.CursorOffset != 6 {
		t.Fatalf("undo 1 failed: got (%q, %d)", snap.Value, snap.CursorOffset)
	}

	// Undo 2: should restore to "hello"
	snap, ok = mgr.Undo(snap.Value, snap.CursorOffset)
	if !ok || snap.Value != "hello" || snap.CursorOffset != 5 {
		t.Fatalf("undo 2 failed: got (%q, %d)", snap.Value, snap.CursorOffset)
	}

	// Undo 3: should restore to ""
	snap, ok = mgr.Undo(snap.Value, snap.CursorOffset)
	if !ok || snap.Value != "" || snap.CursorOffset != 0 {
		t.Fatalf("undo 3 failed: got (%q, %d)", snap.Value, snap.CursorOffset)
	}

	// Redo 1: should restore to "hello"
	snap, ok = mgr.Redo("", 0)
	if !ok || snap.Value != "hello" || snap.CursorOffset != 5 {
		t.Fatalf("redo 1 failed: got (%q, %d)", snap.Value, snap.CursorOffset)
	}

	// Redo 2: should restore to "hello "
	snap, ok = mgr.Redo("hello", 5)
	if !ok || snap.Value != "hello " || snap.CursorOffset != 6 {
		t.Fatalf("redo 2 failed: got (%q, %d)", snap.Value, snap.CursorOffset)
	}

	// Redo 3: should restore to "hello wo"
	snap, ok = mgr.Redo("hello ", 6)
	if !ok || snap.Value != "hello wo" || snap.CursorOffset != 8 {
		t.Fatalf("redo 3 failed: got (%q, %d)", snap.Value, snap.CursorOffset)
	}

	// No more redos
	if mgr.CanRedo() {
		t.Fatalf("expected no redos remaining")
	}
}

func TestUndoManagerDeleteAndPaste(t *testing.T) {
	mgr := NewUndoManager(50)

	// Paste
	mgr.RecordBeforeChange("prefix ", 7, "paste")
	val := "prefix [Pasted text +5 lines]"

	// Delete
	mgr.RecordBeforeChange(val, len(val), "delete")

	if mgr.undoStack.Len() != 2 {
		t.Fatalf("expected 2 snapshots, got %d", mgr.undoStack.Len())
	}

	snap, ok := mgr.Undo("prefix ", 7)
	if !ok || snap.Value != "prefix [Pasted text +5 lines]" {
		t.Fatalf("undo delete failed: got %q", snap.Value)
	}

	snap, ok = mgr.Undo(snap.Value, snap.CursorOffset)
	if !ok || snap.Value != "prefix " {
		t.Fatalf("undo paste failed: got %q", snap.Value)
	}
}

func TestHandleKeyUndoRedo(t *testing.T) {
	e := &fakeEditor{value: ""}
	st := &fakeState{}
	seams := newSeams(e, st, Range{}, nil)
	mgr := NewUndoManager(10)
	seams.UndoMgr = mgr

	// 1. Initial empty buffer: Ctrl+Z should return false (allows SIGTSTP)
	_, handled := HandleKey(seams, tea.Key{Code: 'z', Mod: tea.ModCtrl})
	if handled {
		t.Errorf("Ctrl+Z on empty buffer without undo history should not be handled")
	}

	// 2. Type "hello"
	UpdateTextarea(seams, tea.KeyPressMsg{Code: 'h', Text: "h"})
	e.offset = 1
	UpdateTextarea(seams, tea.KeyPressMsg{Code: 'e', Text: "e"})
	e.offset = 2
	UpdateTextarea(seams, tea.KeyPressMsg{Code: 'l', Text: "l"})
	e.offset = 3
	UpdateTextarea(seams, tea.KeyPressMsg{Code: 'l', Text: "l"})
	e.offset = 4
	UpdateTextarea(seams, tea.KeyPressMsg{Code: 'o', Text: "o"})
	e.offset = 5

	if e.value != "hello" {
		t.Fatalf("expected 'hello', got %q", e.value)
	}

	// 3. Press Cmd+Z to undo
	_, handled = HandleKey(seams, tea.Key{Code: 'z', Mod: tea.ModSuper})
	if !handled {
		t.Fatalf("Cmd+Z should be handled")
	}
	if e.value != "" {
		t.Fatalf("after undo, expected '', got %q", e.value)
	}

	// 4. Press Cmd+Shift+Z to redo
	_, handled = HandleKey(seams, tea.Key{Code: 'z', Mod: tea.ModSuper | tea.ModShift})
	if !handled {
		t.Fatalf("Cmd+Shift+Z should be handled")
	}
	if e.value != "hello" {
		t.Fatalf("after redo, expected 'hello', got %q", e.value)
	}

	// 5. Press Ctrl+Y to redo (nothing more to redo)
	_, handled = HandleKey(seams, tea.Key{Code: 'y', Mod: tea.ModCtrl})
	if !handled {
		t.Fatalf("Ctrl+Y should be handled")
	}
}
