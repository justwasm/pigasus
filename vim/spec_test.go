package vim

import "testing"

// This file pins the behaviour the reference changelog enumerates
// explicitly, including the pieces the first test file did not reach:
// the a/W variants, paragraphs, Visual objects and linewise paste.

// --- Text objects ------------------------------------------------------

func TestPairObjectIsInnermost(t *testing.T) {
	// Cursor on the 'c' inside the inner parens.
	s := newWith(t, "x(a(bc)d)y")
	feed(t, s, "lllll")
	feed(t, s, "di(")
	checkText(t, s, "x(a()d)y") // innermost pair
}

// --- a / W objects ----------------------------------------------------

func TestWordObjectsBig(t *testing.T) {
	s := newWith(t, "foo.bar baz")
	feed(t, s, "diW")
	checkText(t, s, " baz")

	s = newWith(t, "foo.bar baz")
	feed(t, s, "daW")
	checkText(t, s, "baz")
}

func TestParagraphObjects(t *testing.T) {
	s := newWith(t, "a\n\nb")
	feed(t, s, "dip")
	checkText(t, s, "\nb")

	s = newWith(t, "a\n\nb")
	feed(t, s, "dap")
	checkText(t, s, "b")

	// ip on a blank line targets that blank run.
	s = newWith(t, "a\n\nb")
	feed(t, s, "jdip")
	checkText(t, s, "a\nb")
}

// --- Visual objects / Visual Line ------------------------------------

func TestVisualObjects(t *testing.T) {
	// viw selects the whole word even though the cursor is inside it.
	s := newWith(t, "foo bar")
	feed(t, s, "lviwd")
	checkText(t, s, " bar")
	checkMode(t, s, Normal)

	s = newWith(t, "a(bc)d")
	feed(t, s, "lll")
	feed(t, s, "vi(d")
	checkText(t, s, "a()d")

	s = newWith(t, "abc\ndef")
	feed(t, s, "Vjd")
	checkText(t, s, "")
	checkMode(t, s, Normal)
}

func TestVisualLineChange(t *testing.T) {
	s := newWith(t, "abc\ndef")
	feed(t, s, "VcX<esc>")
	checkText(t, s, "X\ndef")
	checkMode(t, s, Normal)
}

// --- Linewise paste ---------------------------------------------------

func TestLinewisePaste(t *testing.T) {
	// yank "def" from line 2, go back up, and P it above line 1.
	s := newWith(t, "abc\ndef")
	feed(t, s, "jyykP")
	checkText(t, s, "def\nabc\ndef")
	checkCursor(t, s, 0, 0)
}

func TestDeleteLineThenPaste(t *testing.T) {
	s := newWith(t, "a\nb\nc")
	feed(t, s, "ddp")
	checkText(t, s, "b\na\nc")
	checkCursor(t, s, 1, 0)
}

// --- Character search with ? -----------------------------------------

func TestFindQuestionMark(t *testing.T) {
	// 1.2.8: f?/t?/df? must work when the prompt has text.
	s := newWith(t, "a?b?c")
	feed(t, s, "df?")
	checkText(t, s, "b?c")
}

// --- Space is not a motion; big-word ends -----------------------------

func TestSpaceAndBigWordEnds(t *testing.T) {
	// The reference does not bind space to a right-motion, so the engine
	// consumes it as an invalid key and leaves the cursor put.
	s := newWith(t, "abc")
	res := s.Handle(Key{Rune: ' '})
	if !res.Consumed {
		t.Fatalf("space: %+v, want consumed", res)
	}
	checkCursor(t, s, 0, 0)
	checkText(t, s, "abc")

	s = newWith(t, "foo.bar baz")
	feed(t, s, "E")
	checkCursor(t, s, 0, 6)
	feed(t, s, "B")
	checkCursor(t, s, 0, 0)
	feed(t, s, "W")
	checkCursor(t, s, 0, 8)
}

// --- o / O open-line entries and ZZ submit ----------------------------

func TestOpenLineEntries(t *testing.T) {
	// o opens a line below and enters Insert.
	s := newWith(t, "ab\ncd")
	feed(t, s, "o")
	checkMode(t, s, Insert)
	checkText(t, s, "ab\n\ncd")
	checkCursor(t, s, 1, 0)
	feed(t, s, "X<esc>")
	checkText(t, s, "ab\nX\ncd")

	// O opens a line above.
	s = newWith(t, "ab")
	feed(t, s, "Ohi<esc>")
	checkText(t, s, "hi\nab")
	checkMode(t, s, Normal)

	// The new line and the text typed into it undo as one step.
	s = newWith(t, "ab")
	feed(t, s, "oxy<esc>")
	checkText(t, s, "ab\nxy")
	feed(t, s, "u")
	checkText(t, s, "ab")
}

func TestZZSubmit(t *testing.T) {
	s := newWith(t, "abc")
	if r := s.Handle(Key{Rune: 'Z'}); !r.Consumed || r.Submit {
		t.Fatalf("lone Z: %+v, want consumed, not submit", r)
	}
	if r := s.Handle(Key{Rune: 'Z'}); !r.Consumed || !r.Submit {
		t.Fatalf("ZZ: %+v, want consumed+submit", r)
	}
	checkText(t, s, "abc")
}

func TestWordEndMovesOn(t *testing.T) {
	// e from a word end advances to the end of the next word, not staying
	// put on the current one.
	s := newWith(t, "foo bar")
	feed(t, s, "e")
	checkCursor(t, s, 0, 2)
	feed(t, s, "e")
	checkCursor(t, s, 0, 6)
}

// --- Undo coalescing --------------------------------------------------

func TestInsertRunUndoAcrossLines(t *testing.T) {
	s := newWith(t, "")
	feed(t, s, "iab<enter>cd<esc>")
	checkText(t, s, "ab\ncd")
	feed(t, s, "u")
	checkText(t, s, "")
	// "U" is the redo key — Ctrl+R opens the artifact review instead
	// (official-docs/cli/vim-editor-mode.md).
	feed(t, s, "U")
	checkText(t, s, "ab\ncd")
}

func TestChangeKeepsIndent(t *testing.T) {
	s := newWith(t, "\t  abc")
	feed(t, s, "ccX<esc>")
	checkText(t, s, "\t  X")
}

// --- Grapheme-safe word motion ----------------------------------------

func TestGraphemeWordMotion(t *testing.T) {
	// The ZWJ emoji is punctuation, so w skips it as one cluster and lands
	// on the 'a'.
	s := newWith(t, "👩‍💻ab cd")
	feed(t, s, "w")
	checkCursor(t, s, 0, 3)
	feed(t, s, "w")
	checkCursor(t, s, 0, 6)
}
