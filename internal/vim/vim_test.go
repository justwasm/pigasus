package vim

import (
	"strings"
	"testing"
)

// parseKeys expands a compact key script into Key values. Named keys are
// written <esc>, <enter>, <bs>, <del>, <tab>, <up>, <down>, <left>,
// <right>, <home>, <end>; chords are <c-s>, <c-r>, <c-j>, and the
// modified Enter variants <s-enter>, <a-enter>, <c-enter>. Everything
// else is a literal rune.
func parseKeys(script string) []Key {
	var out []Key
	for i := 0; i < len(script); {
		if script[i] != '<' {
			r := []rune(script[i:])[0]
			out = append(out, Key{Rune: r})
			i += len(string(r))
			continue
		}
		end := strings.IndexByte(script[i:], '>')
		if end < 0 {
			out = append(out, Key{Rune: '<'})
			i++
			continue
		}
		tok := script[i+1 : i+end]
		i += end + 1
		switch tok {
		case "esc":
			out = append(out, Key{Name: "esc"})
		case "enter":
			out = append(out, Key{Name: "enter"})
		case "bs":
			out = append(out, Key{Name: "backspace"})
		case "del", "delete":
			out = append(out, Key{Name: "delete"})
		case "tab":
			out = append(out, Key{Name: "tab"})
		case "up":
			out = append(out, Key{Name: "up"})
		case "down":
			out = append(out, Key{Name: "down"})
		case "left":
			out = append(out, Key{Name: "left"})
		case "right":
			out = append(out, Key{Name: "right"})
		case "home":
			out = append(out, Key{Name: "home"})
		case "end":
			out = append(out, Key{Name: "end"})
		case "space":
			out = append(out, Key{Rune: ' '})
		case "c-s":
			out = append(out, Key{Rune: 's', Mod: Ctrl})
		case "c-r":
			out = append(out, Key{Rune: 'r', Mod: Ctrl})
		case "c-j":
			out = append(out, Key{Rune: 'j', Mod: Ctrl})
		case "c-enter":
			out = append(out, Key{Name: "enter", Mod: Ctrl})
		case "s-enter":
			out = append(out, Key{Name: "enter", Mod: Shift})
		case "a-enter":
			out = append(out, Key{Name: "enter", Mod: Alt})
		default:
			for _, r := range tok {
				out = append(out, Key{Rune: r})
			}
		}
	}
	return out
}

// feed runs a key script and returns the final state.
func feed(t *testing.T, s *State, script string) *State {
	t.Helper()
	for _, k := range parseKeys(script) {
		s.Handle(k)
	}
	return s
}

func newWith(t *testing.T, text string) *State {
	t.Helper()
	return New(text)
}

func checkText(t *testing.T, s *State, want string) {
	t.Helper()
	if got := s.Text(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func checkCursor(t *testing.T, s *State, row, col int) {
	t.Helper()
	if s.Row != row || s.Col != col {
		t.Fatalf("cursor = (%d,%d), want (%d,%d)", s.Row, s.Col, row, col)
	}
}

func checkMode(t *testing.T, s *State, want Mode) {
	t.Helper()
	if s.Mode != want {
		t.Fatalf("mode = %v, want %v", s.Mode, want)
	}
}

// --- Insert mode ------------------------------------------------------

func TestInsertTypingAndEscape(t *testing.T) {
	s := newWith(t, "")
	checkMode(t, s, Normal)
	feed(t, s, "ihello")
	checkText(t, s, "hello")
	checkCursor(t, s, 0, 5)
	feed(t, s, "<esc>")
	checkMode(t, s, Normal)
	// Esc pulls the cursor back onto the last character.
	checkCursor(t, s, 0, 4)
}

func TestInsertEntries(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "liX<esc>")
	checkText(t, s, "aXbc")

	s = newWith(t, "abc")
	feed(t, s, "aX<esc>")
	checkText(t, s, "aXbc")

	s = newWith(t, "abc")
	feed(t, s, "AX<esc>")
	checkText(t, s, "abcX")

	s = newWith(t, "  abc")
	feed(t, s, "lIX<esc>")
	checkText(t, s, "  Xabc")
}

func TestInsertNewlineAndBackspace(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "i")
	checkText(t, s, "abc")
	feed(t, s, "<bs>")
	checkText(t, s, "abc") // backspace at col 0 on row 0 is a no-op
	feed(t, s, "<right><bs>")
	checkText(t, s, "bc")

	s = newWith(t, "ab\ncd")
	feed(t, s, "A<enter>x<esc>")
	checkText(t, s, "ab\nx\ncd")

	s = newWith(t, "ab\ncd")
	feed(t, s, "ji<bs>")
	checkText(t, s, "abcd")
}

// --- Motions ----------------------------------------------------------

func TestBasicMotions(t *testing.T) {
	s := newWith(t, "hello")
	feed(t, s, "lll")
	checkCursor(t, s, 0, 3)
	feed(t, s, "h")
	checkCursor(t, s, 0, 2)
	// l stops on the last character.
	feed(t, s, "999l")
	checkCursor(t, s, 0, 4)

	s = newWith(t, "aaa\nbbb\nccc")
	feed(t, s, "j")
	checkCursor(t, s, 1, 0)
	feed(t, s, "j")
	checkCursor(t, s, 2, 0)
	feed(t, s, "k")
	checkCursor(t, s, 1, 0)
}

func TestBoundaries(t *testing.T) {
	s := newWith(t, "  hello world")
	feed(t, s, "$")
	checkCursor(t, s, 0, 12)
	feed(t, s, "0")
	checkCursor(t, s, 0, 0)
	feed(t, s, "^")
	checkCursor(t, s, 0, 2)

	s = newWith(t, "aaa\nbbb\nccc")
	feed(t, s, "G")
	checkCursor(t, s, 2, 0)
	feed(t, s, "gg")
	checkCursor(t, s, 0, 0)
	feed(t, s, "2G")
	checkCursor(t, s, 1, 0)
	feed(t, s, "3gg")
	checkCursor(t, s, 2, 0)
}

func TestWordMotions(t *testing.T) {
	s := newWith(t, "foo bar baz")
	feed(t, s, "w")
	checkCursor(t, s, 0, 4)
	feed(t, s, "w")
	checkCursor(t, s, 0, 8)
	feed(t, s, "b")
	checkCursor(t, s, 0, 4)
	feed(t, s, "e")
	checkCursor(t, s, 0, 6)

	// Word vs WORD: punctuation splits w but not W.
	s = newWith(t, "foo.bar baz")
	feed(t, s, "w")
	checkCursor(t, s, 0, 3) // stops on the '.'
	s = newWith(t, "foo.bar baz")
	feed(t, s, "W")
	checkCursor(t, s, 0, 8)

	// Cross-line motion.
	s = newWith(t, "foo\nbar")
	feed(t, s, "w")
	checkCursor(t, s, 1, 0)
}

func TestFindMotion(t *testing.T) {
	s := newWith(t, "abcabc")
	feed(t, s, "fc")
	checkCursor(t, s, 0, 2)
	feed(t, s, ";")
	checkCursor(t, s, 0, 5)
	feed(t, s, ",")
	checkCursor(t, s, 0, 2)
	feed(t, s, "tc")
	checkCursor(t, s, 0, 4)
	s = newWith(t, "abcabc")
	feed(t, s, "$")
	feed(t, s, "Fc")
	checkCursor(t, s, 0, 2)
}

// --- Edits ------------------------------------------------------------

func TestDeleteChar(t *testing.T) {
	s := newWith(t, "abcd")
	feed(t, s, "x")
	checkText(t, s, "bcd")
	checkCursor(t, s, 0, 0)
	s = newWith(t, "abcd")
	feed(t, s, "llx")
	checkText(t, s, "abd")
	checkCursor(t, s, 0, 2)

	s = newWith(t, "abcd")
	feed(t, s, "llX")
	checkText(t, s, "acd")
	checkCursor(t, s, 0, 1)
}

func TestDeleteToEndAndChangeToEnd(t *testing.T) {
	s := newWith(t, "abcde")
	feed(t, s, "llD")
	checkText(t, s, "ab")
	checkCursor(t, s, 0, 1)

	s = newWith(t, "abcde")
	feed(t, s, "llC")
	checkText(t, s, "ab")
	checkMode(t, s, Insert)
	checkCursor(t, s, 0, 2)
}

func TestOperators(t *testing.T) {
	cases := []struct {
		name string
		text string
		keys string
		want string
		row  int
		col  int
	}{
		{"dw", "foo bar", "dw", "bar", 0, 0},
		{"de", "foo bar", "de", " bar", 0, 0},
		{"d$", "foo bar", "d$", "", 0, 0},
		{"dd", "a\nb\nc", "dd", "b\nc", 0, 0},
		{"dw last word", "foo", "dw", "", 0, 0},
		{"x then p", "abc", "xp", "bac", 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newWith(t, c.text)
			feed(t, s, c.keys)
			checkText(t, s, c.want)
			checkCursor(t, s, c.row, c.col)
		})
	}
}

func TestYankAndPaste(t *testing.T) {
	s := newWith(t, "abc\ndef")
	feed(t, s, "yyp")
	checkText(t, s, "abc\nabc\ndef")
	checkCursor(t, s, 1, 0)

	// P pastes before the cursor; x fills the register first.
	s = newWith(t, "abcd")
	feed(t, s, "xP")
	checkText(t, s, "abcd")
	checkCursor(t, s, 0, 0)
}

func TestChange(t *testing.T) {
	s := newWith(t, "foo bar")
	feed(t, s, "cwX<esc>")
	checkText(t, s, "X bar")
	checkMode(t, s, Normal)

	s = newWith(t, "foo bar")
	feed(t, s, "ciwX<esc>")
	checkText(t, s, "X bar")

	s = newWith(t, "a\nb\nc")
	feed(t, s, "ccX<esc>")
	checkText(t, s, "X\nb\nc")
}

func TestTextObjects(t *testing.T) {
	s := newWith(t, "foo bar")
	feed(t, s, "diw")
	checkText(t, s, " bar")

	s = newWith(t, "foo bar")
	feed(t, s, "daw")
	checkText(t, s, "bar")

	s = newWith(t, "a(bc)d")
	feed(t, s, "lll")
	feed(t, s, "di(")
	checkText(t, s, "a()d")

	s = newWith(t, `say "hi" now`)
	feed(t, s, "ww")
	feed(t, s, `di"`)
	checkText(t, s, `say "" now`)

	// dip deletes the whole paragraph (the run of non-blank lines).
	s = newWith(t, "one two\nthree four\n\nfive six")
	feed(t, s, "jdip")
	checkText(t, s, "\nfive six")
}

func TestVisual(t *testing.T) {
	s := newWith(t, "abc\ndef")
	feed(t, s, "vld")
	checkText(t, s, "c\ndef")
	checkMode(t, s, Normal)
	checkCursor(t, s, 0, 0)

	s = newWith(t, "abc\ndef")
	feed(t, s, "Vd")
	checkText(t, s, "def")

	s = newWith(t, "abc")
	feed(t, s, "vlly")
	checkText(t, s, "abc")
	checkMode(t, s, Normal)
	if s.register != "abc" {
		t.Fatalf("register = %q, want abc", s.register)
	}
}

func TestVisualCase(t *testing.T) {
	s := newWith(t, "hello")
	feed(t, s, "vll~")
	checkText(t, s, "HELlo")
	checkMode(t, s, Normal)

	s = newWith(t, "HIs")
	feed(t, s, "vllu")
	checkText(t, s, "his")
}

// --- Counts -----------------------------------------------------------

func TestCounts(t *testing.T) {
	s := newWith(t, "aaaaaa")
	feed(t, s, "3x")
	checkText(t, s, "aaa")

	s = newWith(t, "a b c d")
	feed(t, s, "2dw")
	checkText(t, s, "c d")

	s = newWith(t, "a\nb\nc\nd")
	feed(t, s, "3dd")
	checkText(t, s, "d")

	s = newWith(t, "abcdef")
	feed(t, s, "3rX")
	checkText(t, s, "XXXdef")

	s = newWith(t, "abc")
	feed(t, s, "yy3p")
	checkText(t, s, "abc\nabc\nabc\nabc")

	s = newWith(t, "abcdef")
	feed(t, s, "xx3u")
	checkText(t, s, "abcdef")
}

// --- Undo/redo coalescing --------------------------------------------

func TestUndoRedo(t *testing.T) {
	s := newWith(t, "")
	feed(t, s, "ihello<esc>")
	checkText(t, s, "hello")
	feed(t, s, "u")
	checkText(t, s, "") // the whole insert run undoes as one step
	feed(t, s, "<c-r>")
	checkText(t, s, "hello")
}

// --- Grapheme safety --------------------------------------------------

func TestGraphemeSafeMotion(t *testing.T) {
	s := newWith(t, "👩‍💻ab")
	feed(t, s, "l")
	checkCursor(t, s, 0, 3) // the ZWJ cluster is one step
	feed(t, s, "x")
	checkText(t, s, "👩‍💻b")

	s = newWith(t, "e\u0301x") // e + combining acute = one cluster
	feed(t, s, "x")
	checkText(t, s, "x")
}

// --- Submit bindings --------------------------------------------------

func TestSubmitBindings(t *testing.T) {
	s := newWith(t, "hi")
	res := s.Handle(Key{Rune: 's', Mod: Ctrl})
	if !res.Submit || !res.Consumed {
		t.Fatalf("normal ctrl+s: %+v, want submit", res)
	}
	s = newWith(t, "hi")
	s.Mode = Insert
	res = s.Handle(Key{Rune: 's', Mod: Ctrl})
	if !res.Submit {
		t.Fatalf("insert ctrl+s: %+v, want submit", res)
	}
}

func TestInsertFirst(t *testing.T) {
	s := New("hi")
	s.InsertFirst = true
	s.Mode = Insert
	s.Col = s.lineLen(s.Row)
	s.desiredCol = s.Col
	res := s.Handle(Key{Name: "enter"})
	if !res.Submit {
		t.Fatalf("insert-first enter: %+v, want submit", res)
	}
	// A modified Enter still inserts a newline.
	res = s.Handle(Key{Name: "enter", Mod: Shift})
	if res.Submit {
		t.Fatalf("shift+enter should not submit")
	}
	checkText(t, s, "hi\n")
}

func TestBadge(t *testing.T) {
	cases := map[Mode]string{
		Normal:     "",
		Insert:     "-- INSERT --",
		Visual:     "-- VISUAL --",
		VisualLine: "-- V-LINE --",
	}
	for m, want := range cases {
		if got := m.Badge(); got != want {
			t.Errorf("badge(%v) = %q, want %q", m, got, want)
		}
	}
}
