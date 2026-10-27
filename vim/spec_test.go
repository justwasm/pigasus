package vim

import "testing"

// This file pins the behaviour the reference changelog enumerates
// explicitly, including the pieces the first test file did not reach:
// counted text objects, the a/W variants, paragraphs, Visual objects,
// linewise paste and the count multipliers listed under 1.2.9.

// --- Counted text objects (1.2.9: "2di(") ----------------------------

func TestCountedTextObjects(t *testing.T) {
	// Cursor on the 'c' inside the inner parens.
	s := newWith(t, "x(a(bc)d)y")
	feed(t, s, "lllll")
	feed(t, s, "di(")
	checkText(t, s, "x(a()d)y") // innermost pair

	s = newWith(t, "x(a(bc)d)y")
	feed(t, s, "lllll")
	feed(t, s, "2di(")
	checkText(t, s, "x()y") // one level out

	// The count may also lead the operator, and clamps at the outermost
	// pair rather than failing.
	s = newWith(t, "x(a(bc)d)y")
	feed(t, s, "lllll")
	feed(t, s, "d3i(")
	checkText(t, s, "x()y")
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

// --- Count multipliers (1.2.9) ---------------------------------------

func TestCountMultipliers(t *testing.T) {
	// 3x fills the register with the deleted text.
	s := newWith(t, "abcdef")
	feed(t, s, "3x")
	checkText(t, s, "def")
	if s.register != "abc" {
		t.Fatalf("register = %q, want abc", s.register)
	}

	// [count]$ moves count-1 lines down and to the end of line.
	s = newWith(t, "ab\ncd")
	feed(t, s, "2$")
	checkCursor(t, s, 1, 1)

	// [count]G and [count]gg are absolute line numbers.
	s = newWith(t, "a\nb\nc\nd")
	feed(t, s, "3G")
	checkCursor(t, s, 2, 0)
	feed(t, s, "1gg")
	checkCursor(t, s, 0, 0)

	// 2d3w multiplies: the op count and the motion count combine to 6,
	// deleting six of the eight words.
	s = newWith(t, "a b c d e f g h")
	feed(t, s, "2d3w")
	checkText(t, s, "g h")

	// d5w leaves the sixth word.
	s = newWith(t, "a b c d e f")
	feed(t, s, "5dw")
	checkText(t, s, "f")

	// A counted object with a leading operator count too.
	s = newWith(t, "abcdef")
	feed(t, s, "2rZ")
	checkText(t, s, "ZZcdef")
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
	feed(t, s, "<c-r>")
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
