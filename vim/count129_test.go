package vim

import "testing"

// AGY 1.2.9 changelog gap closure — Vim numeric count multipliers.
//
// The reference's 1.2.9 changelog promises:
//
//   "Added Vim numeric count multipliers so counts apply to operators,
//    motions, and actions in Normal and Visual modes, including 3dw,
//    2d3w, 3dd, 3x, 3rX, 3p, 3u, [count]G/gg/$, and counted text
//    objects such as 2di(."
//
// Cycle 215 (vim/vim.go, parse.go, engine.go, motion.go,
// object.go, operator.go) added the parser-state-machine machinery
// that turns a leading digit run into the count field of every command
// kind. This file pins the changelog examples to test names so the
// gap is hard to regress.
//
// The 5 minimal cases the gap closure requires are the first 5
// tests below. The remaining tests cover the explicit list (2d3w,
// [count]G/gg/$, 3u) and a few edge cases the original
// TestCounts/TestCountMultipliers left implicit.

// 3dw deletes 3 words.
func TestCount129_3dwDeletesThreeWords(t *testing.T) {
	s := newWith(t, "alpha beta gamma delta epsilon zeta")
	feed(t, s, "3dw")
	checkText(t, s, "delta epsilon zeta")
	checkCursor(t, s, 0, 0)
}

// 3dd deletes 3 lines.
func TestCount129_3ddDeletesThreeLines(t *testing.T) {
	s := newWith(t, "line1\nline2\nline3\nline4\nline5")
	feed(t, s, "3dd")
	checkText(t, s, "line4\nline5")
	checkCursor(t, s, 0, 0)
}

// 3x deletes 3 chars.
func TestCount129_3xDeletesThreeChars(t *testing.T) {
	s := newWith(t, "abcdefgh")
	feed(t, s, "3x")
	checkText(t, s, "defgh")
	checkCursor(t, s, 0, 0)
	if s.register != "abc" {
		t.Fatalf("register = %q, want abc", s.register)
	}
}

// 3p pastes 3 times (the unnamed register holds the original line once
// — yanking it, then pasting 3 times, yields 4 copies total).
func TestCount129_3pPastesThreeTimes(t *testing.T) {
	s := newWith(t, "yank")
	feed(t, s, "yy3p")
	checkText(t, s, "yank\nyank\nyank\nyank")
}

// 2di( deletes inside the outer of two enclosing pairs.
func TestCount129_2diParenDeletesInsideParensTwice(t *testing.T) {
	s := newWith(t, "x(a(bc)d)y")
	// 5 l's lands the cursor on 'c', the inner parens' contents.
	feed(t, s, "lllll")
	feed(t, s, "2di(")
	checkText(t, s, "x()y")
}

// The remaining tests pin the rest of the changelog's explicit list.

// 2d3w multiplies the operator count and the motion count.
func TestCount129_2d3wMultipliesCounts(t *testing.T) {
	s := newWith(t, "a b c d e f g h")
	feed(t, s, "2d3w")
	checkText(t, s, "g h")
}

// 3rX replaces 3 characters with 'X'.
func TestCount129_3rXReplacesThree(t *testing.T) {
	s := newWith(t, "abcdef")
	feed(t, s, "3rX")
	checkText(t, s, "XXXdef")
}

// 3u undoes 3 times (clamped at the available undo steps).
func TestCount129_3uUndoesThreeTimes(t *testing.T) {
	s := newWith(t, "abcdef")
	feed(t, s, "xx3u")
	checkText(t, s, "abcdef")
}

// [count]G is an absolute line jump.
func TestCount129_CountGIsAbsolute(t *testing.T) {
	s := newWith(t, "a\nb\nc\nd\ne")
	feed(t, s, "3G")
	checkCursor(t, s, 2, 0)
}

// [count]gg is also an absolute line jump.
func TestCount129_CountGgIsAbsolute(t *testing.T) {
	s := newWith(t, "a\nb\nc\nd\ne")
	feed(t, s, "4gg")
	checkCursor(t, s, 3, 0)
}

// [count]$ moves count-1 lines down and to the end of that line.
func TestCount129_CountDollarMovesDownLines(t *testing.T) {
	s := newWith(t, "ab\ncd\nef\ngh")
	feed(t, s, "3$")
	checkCursor(t, s, 2, 1)
}

// The leading-zero rule: "0" is always the line-start motion, never a
// count prefix, so 0w must behave like w (count defaults to 1). The
// existing parser deliberately rejects "0" as a count digit so "0"
// stays available as the line-start motion.
func TestCount129_LeadingZeroIsNotCount(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "0")
	checkCursor(t, s, 0, 0)
	// 0w is parsed as line-start (col 0) + w (no count), which on a
	// single-word line lands on the last char rather than moving.
	feed(t, s, "0w")
	checkCursor(t, s, 0, 2)

	// For contrast, an explicit count prefix moves the same w further:
	s = newWith(t, "abc def")
	feed(t, s, "0w")
	checkCursor(t, s, 0, 4) // "0" line-start, then "w" lands on "def"
}

// A multi-digit count (10j) and its clamp at the last line.
func TestCount129_MultiDigitCountClampsAtBufferEnd(t *testing.T) {
	s := newWith(t, "a\nb\nc")
	feed(t, s, "100j")
	checkCursor(t, s, 2, 0)

	s = newWith(t, stringsFromLines("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"))
	feed(t, s, "10j")
	checkCursor(t, s, 10, 0)
}

// The parser does not eat digits that begin an unrelated sequence: a
// bare "3" with no following motion must be left as an incomplete
// state, not silently turn into a count-then-empty command. Once a
// real motion arrives, the count attaches to it.
func TestCount129_BareCountStaysPending(t *testing.T) {
	s := newWith(t, "alpha beta gamma delta")
	res := s.Handle(Key{Rune: '3'})
	if !res.Consumed {
		t.Fatalf("3 not consumed")
	}
	if s.Pending != "3" {
		t.Fatalf("pending = %q, want 3", s.Pending)
	}
	// The count completes only once a real motion/operator arrives:
	// 3w from (0,0) lands on the third word's start.
	feed(t, s, "w")
	checkCursor(t, s, 0, 17) // start of "delta" (skip alpha, beta, gamma)

	// Same shape with a buffered "2" then a different motion: 2j jumps
	// two lines down from wherever the cursor is now.
	s = newWith(t, "a\nb\nc\nd")
	feed(t, s, "2j")
	checkCursor(t, s, 2, 0)
}

// stringsFromLines is a tiny helper so the multi-digit test reads
// naturally without dragging strings.Split into the test surface.
func stringsFromLines(lines ...string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
