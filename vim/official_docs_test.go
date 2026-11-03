package vim

import "testing"

// The bindings this file pins are the ones
// official-docs/cli/vim-editor-mode.md states explicitly:
//
//   - "u U | Undo, redo. Ctrl+R does not redo; it opens artifact review"
//   - "r{char} | Replace every selected character with {char}" (VISUAL)
//   - "Press / or ! in NORMAL mode. The CLI inserts the character and
//     switches to INSERT mode, so slash commands and shell commands work
//     without pressing i first."
//
// and the ones it states about the mode as a whole:
//
//   - "In NORMAL mode, `u` and `U` are undo and redo." (the note under
//     the selection table — the VISUAL meanings are claimed first)
//
// The documentation names the two keys and not the arithmetic behind
// them, so the pins below also record what the clone's redo stack does
// with a run of steps, with an empty stack and with a new edit. Those
// are the clone's reading, and they are pinned so a later change cannot
// quietly move them.

// TestCtrlRDoesNotRedo: Ctrl+R is consumed, but it leaves the undo and
// the redo stack alone — the reference opens the artifact review with it
// (the /help catalogue says "ctrl+r  Review artifact").
func TestCtrlRDoesNotRedo(t *testing.T) {
	s := newWith(t, "")
	feed(t, s, "ihello<esc>")
	feed(t, s, "u")
	checkText(t, s, "")

	res := s.Handle(Key{Rune: 'r', Mod: Ctrl})
	if !res.Consumed {
		t.Fatalf("ctrl+r: %+v, want consumed", res)
	}
	checkText(t, s, "")
	checkMode(t, s, Normal)

	// The redo is still on the stack, so "U" brings the text back.
	feed(t, s, "U")
	checkText(t, s, "hello")
}

// TestUppercaseRedo: "U" is the redo in NORMAL mode.
func TestUppercaseRedo(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "x")
	checkText(t, s, "bc")
	feed(t, s, "u")
	checkText(t, s, "abc")
	feed(t, s, "U")
	checkText(t, s, "bc")
}

// TestUppercaseIsVisualOnly: "U" uppercases a VISUAL selection and
// redoes in NORMAL mode — the two meanings are mode-dependent, and the
// VISUAL one is claimed before the NORMAL one.
func TestUppercaseIsVisualOnly(t *testing.T) {
	s := newWith(t, "hello")
	feed(t, s, "vllU")
	checkText(t, s, "HELlo")
	checkMode(t, s, Normal)

	s = newWith(t, "abc")
	feed(t, s, "x")
	feed(t, s, "u")
	feed(t, s, "U")
	checkText(t, s, "bc")
}

// TestSlashAndBangEnterInsertMode: the reference's NORMAL-mode "/" and
// "!" insert the character and switch to INSERT mode.
func TestSlashAndBangEnterInsertMode(t *testing.T) {
	s := newWith(t, "")
	feed(t, s, "/")
	checkText(t, s, "/")
	checkMode(t, s, Insert)
	checkCursor(t, s, 0, 1)

	s = newWith(t, "")
	feed(t, s, "!")
	checkText(t, s, "!")
	checkMode(t, s, Insert)
	checkCursor(t, s, 0, 1)

	// The character is inserted at the cursor, so the rest of the
	// command is typed straight after it.
	s = newWith(t, "")
	feed(t, s, "/settings")
	checkText(t, s, "/settings")
	checkMode(t, s, Insert)
}

// TestSlashIsNormalModeOnly: in VISUAL mode the "/" and the "!" are not
// insert entries — they would drop the selection.
func TestSlashIsNormalModeOnly(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "v")
	checkMode(t, s, Visual)
	res := s.Handle(Key{Rune: '/'})
	if !res.Bell {
		t.Fatalf("visual /: %+v, want a bell", res)
	}
	if s.Mode != Visual {
		t.Fatalf("mode = %v, want %v", s.Mode, Visual)
	}
	checkText(t, s, "abc")
}

// TestVisualReplace: r{char} replaces every selected character.
func TestVisualReplace(t *testing.T) {
	s := newWith(t, "hello")
	feed(t, s, "vllrX")
	checkText(t, s, "XXXlo")
	checkMode(t, s, Normal)
	checkCursor(t, s, 0, 0)

	// The replacement is one undo step.
	feed(t, s, "u")
	checkText(t, s, "hello")

	// A line-wise selection replaces every character of every selected
	// line, and the lines keep their length.
	s = newWith(t, "ab\ncd")
	feed(t, s, "VjrZ")
	checkText(t, s, "ZZ\nZZ")
	checkMode(t, s, Normal)
}

// TestNormalInsertEntriesUnchanged: the two insert entries the
// documentation adds ("/" and "!") did not change the older ones.
//
// "i" still enters INSERT before the character under the cursor, "a"
// after it, "I" at the first non-blank, "A" at the end of the line, and
// "o" / "O" open a line below / above.
func TestNormalInsertEntriesUnchanged(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "i")
	checkMode(t, s, Insert)
	checkCursor(t, s, 0, 0)

	s = newWith(t, "abc")
	feed(t, s, "a")
	checkMode(t, s, Insert)
	checkCursor(t, s, 0, 1)

	s = newWith(t, "  abc")
	feed(t, s, "I")
	checkMode(t, s, Insert)
	checkCursor(t, s, 0, 2)

	s = newWith(t, "abc")
	feed(t, s, "A")
	checkMode(t, s, Insert)
	checkCursor(t, s, 0, 3)

	s = newWith(t, "ab")
	feed(t, s, "o")
	checkMode(t, s, Insert)
	checkText(t, s, "ab\n")
	checkCursor(t, s, 1, 0)

	s = newWith(t, "ab")
	feed(t, s, "O")
	checkMode(t, s, Insert)
	checkText(t, s, "\nab")
	checkCursor(t, s, 0, 0)
}

// TestInvalidKeysStillBellInNormalMode: the parser is not more
// permissive than it was. A key that is not bound is consumed with a
// bell, the buffer is untouched, and no half-typed sequence is left
// behind.
//
// The set is the keys the documentation never claims. "?" is the host's
// shortcuts overlay key — it is claimed before the engine ever sees it
// — but the engine's own parser does not know it either. "%" and "#"
// are not commands in any mode.
func TestInvalidKeysStillBellInNormalMode(t *testing.T) {
	for _, r := range []rune{'?', '%', '#'} {
		s := newWith(t, "abc")
		res := s.Handle(Key{Rune: r})
		if !res.Consumed || !res.Bell {
			t.Fatalf("%q: %+v, want consumed+bell", r, res)
		}
		checkText(t, s, "abc")
		checkMode(t, s, Normal)
		if s.Pending != "" {
			t.Fatalf("%q: pending = %q, want empty", r, s.Pending)
		}
	}
}

// TestRedoIsASeparateStack: the documentation says "u U | Undo, redo"
// and nothing more, so the clone's own reading is pinned here.
//
// The redo is a **stack of its own**: "u" pushes the step it undid onto
// it and "U" pops it. The steps come back in the order they were
// undone — most recent first — so one "U" repeats exactly one step and
// never a run of them.
func TestRedoIsASeparateStack(t *testing.T) {
	// Two edits, two undos, two redos: the redos walk back through the
	// undos in the reverse order.
	s := newWith(t, "abc")
	feed(t, s, "xx")
	checkText(t, s, "c")
	feed(t, s, "uu")
	checkText(t, s, "abc")
	feed(t, s, "U")
	checkText(t, s, "bc")
	feed(t, s, "U")
	checkText(t, s, "c")

	// A new edit empties the redo stack, so the "U" after it repeats
	// nothing — and it does not repeat the *old* step either. The
	// second edit changes the text to something else, so a redo that
	// still held the first step would put the old text back.
	s = newWith(t, "abc")
	feed(t, s, "x")
	feed(t, s, "u")
	checkText(t, s, "abc")
	feed(t, s, "iZ<esc>")
	checkText(t, s, "Zabc")
	feed(t, s, "U")
	checkText(t, s, "Zabc")

	// An empty redo stack is not an error: the key is consumed and
	// nothing rings the bell.
	s = newWith(t, "abc")
	res := s.Handle(Key{Rune: 'U'})
	if !res.Consumed || res.Bell {
		t.Fatalf("U on an empty redo stack: %+v, want consumed without a bell", res)
	}
	checkText(t, s, "abc")
	checkMode(t, s, Normal)
}

// TestSlashInsertEntryJoinsTheInsertRun: the "/" reuses the insert
// mode's rune path, so the character and the text typed after it are
// **one** undo step — "/settings" is a single undo, not eight
// keystrokes.
func TestSlashInsertEntryJoinsTheInsertRun(t *testing.T) {
	s := newWith(t, "")
	feed(t, s, "/settings<esc>")
	checkText(t, s, "/settings")
	feed(t, s, "u")
	checkText(t, s, "")
	feed(t, s, "U")
	checkText(t, s, "/settings")
}

// TestSlashInsertEntryInsertsAtTheCursor: the character goes in at the
// cursor, before the character under it — the same place "i" would put
// it — and the cursor lands after it, in INSERT mode.
func TestSlashInsertEntryInsertsAtTheCursor(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "/")
	checkText(t, s, "/abc")
	checkCursor(t, s, 0, 1)
	checkMode(t, s, Insert)

	s = newWith(t, "abc")
	feed(t, s, "!")
	checkText(t, s, "!abc")
	checkCursor(t, s, 0, 1)
	checkMode(t, s, Insert)
}
