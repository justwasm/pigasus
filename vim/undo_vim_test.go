package vim

import (
	"testing"
)

// TestVim1511_ChangeWordUndoUnit verifies AGY 1.5.11:
// "a change that enters Insert mode, such as cw or o, undoes together with
// everything you typed before Esc".
func TestVim1511_ChangeWordUndoUnit(t *testing.T) {
	s := newWith(t, "hello world again")
	// Move cursor to "world"
	feed(t, s, "w")
	if s.Col != 6 {
		t.Fatalf("cursor at col %d, want 6", s.Col)
	}

	// cw -> replace "world" with "earth" -> Esc
	feed(t, s, "cw")
	if s.Mode != Insert {
		t.Fatalf("mode = %v, want Insert", s.Mode)
	}
	feed(t, s, "earth")
	s.Handle(Key{Name: "esc"})
	if s.Mode != Normal {
		t.Fatalf("mode = %v, want Normal after Esc", s.Mode)
	}
	checkText(t, s, "hello earth again")

	// Single 'u' must undo the entire cw + typed text back to "hello world again"
	feed(t, s, "u")
	checkText(t, s, "hello world again")
	if s.Col != 6 {
		t.Errorf("cursor after undoing cw = col %d, want start of changed text (6)", s.Col)
	}
}

// TestVim1511_OpenLineUndoUnit verifies:
// "o undoes together with everything you typed before Esc".
func TestVim1511_OpenLineUndoUnit(t *testing.T) {
	s := newWith(t, "first line")
	feed(t, s, "o")
	if s.Mode != Insert {
		t.Fatalf("mode = %v, want Insert", s.Mode)
	}
	feed(t, s, "second line")
	s.Handle(Key{Name: "esc"})
	checkText(t, s, "first line\nsecond line")

	// Single 'u' must undo the entire openLine + typed text
	feed(t, s, "u")
	checkText(t, s, "first line")
}

// TestVim1511_OperatorCursorPlacement verifies:
// "undoing or redoing an operator such as dd or diw puts the cursor at the
// start of the changed text."
func TestVim1511_OperatorCursorPlacement(t *testing.T) {
	t.Run("diw cursor placement", func(t *testing.T) {
		s := newWith(t, "hello universe today")
		// Move cursor inside "universe": e.g. at col 9 ('i')
		s.Col = 9
		feed(t, s, "diw")
		checkText(t, s, "hello  today")

		// Undo: cursor must be at start of "universe" (col 6)
		feed(t, s, "u")
		checkText(t, s, "hello universe today")
		if s.Col != 6 {
			t.Errorf("cursor after undoing diw = col %d, want start of changed text (6)", s.Col)
		}

		// Redo: cursor must remain at start of changed text (col 6)
		feed(t, s, "U")
		checkText(t, s, "hello  today")
		if s.Col != 6 {
			t.Errorf("cursor after redoing diw = col %d, want start of changed text (6)", s.Col)
		}
	})

	t.Run("dd cursor placement", func(t *testing.T) {
		s := newWith(t, "line one\nsecond line with content\nline three")
		// Cursor on second line, column 12
		s.Row = 1
		s.Col = 12
		feed(t, s, "dd")
		checkText(t, s, "line one\nline three")

		// Undo: cursor must be restored to start of line (row 1, col 0)
		feed(t, s, "u")
		checkText(t, s, "line one\nsecond line with content\nline three")
		if s.Row != 1 || s.Col != 0 {
			t.Errorf("cursor after undoing dd = row %d, col %d, want (1, 0)", s.Row, s.Col)
		}
	})
}

// TestVim1511_OneCommandAtATime verifies:
// "u now undoes one command at a time instead of grouping edits made close together".
func TestVim1511_OneCommandAtATime(t *testing.T) {
	s := newWith(t, "abcdef")
	// Three separate 'x' commands
	feed(t, s, "xxx")
	checkText(t, s, "def")

	// Each 'u' undoes one 'x'
	feed(t, s, "u")
	checkText(t, s, "cdef")

	feed(t, s, "u")
	checkText(t, s, "bcdef")

	feed(t, s, "u")
	checkText(t, s, "abcdef")
}
