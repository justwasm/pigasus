package vim

import "testing"

// The reference's commands take no count prefix.
//
// 【一手】 official-docs/cli/vim-editor-mode.md: "Commands take no count
// prefix, so `3dd` deletes one line. To act on several lines at once,
// select them with `V` and press the operator."
//
// This file pins that rule: a leading digit is an invalid key (it rings
// the bell and is dropped), and every command acts once.
//
// 存疑: the reference's own 1.2.9 changelog announced the opposite —
// "Added Vim numeric count multipliers so counts apply to operators,
// motions, and actions in Normal and Visual modes, including `3dw`,
// `2d3w`, `3dd`, `3x`, `3rX`, `3p`, `3u`, `[count]G`/`gg`/`$`, and
// counted text objects such as `2di(`" (official-docs/changelog.md,
// v1.2.9) — and the 1.4.11 binary still carries the count-aware
// helpers `vim.extendWordRangeForCount` and
// `vim.extendAroundWordRangeForCount`
// (out/local-macos/seeds/seeds.txt). The documentation is the newer
// statement about the current behaviour, so it wins here; the counts
// were dropped from the clone with this file.

// TestNoCountPrefixDeletesOneLine: "3dd" deletes one line — the digit
// rings the bell and the "dd" that follows deletes a single line.
func TestNoCountPrefixDeletesOneLine(t *testing.T) {
	s := newWith(t, "line1\nline2\nline3\nline4\nline5")
	feed(t, s, "3dd")
	checkText(t, s, "line2\nline3\nline4\nline5")
	checkCursor(t, s, 0, 0)
}

// TestCountDigitRingsTheBell: a leading digit is an invalid key, so it
// is consumed with a bell and the buffer is untouched.
func TestCountDigitRingsTheBell(t *testing.T) {
	s := newWith(t, "abc")
	res := s.Handle(Key{Rune: '3'})
	if !res.Consumed || !res.Bell {
		t.Fatalf("3: %+v, want consumed+bell", res)
	}
	checkText(t, s, "abc")
	checkCursor(t, s, 0, 0)
	if s.Pending != "" {
		t.Fatalf("pending = %q, want empty", s.Pending)
	}
}

// TestNoCountPrefixActsOnce: every command acts once, whatever digit
// precedes it.
func TestNoCountPrefixActsOnce(t *testing.T) {
	// 3x deletes one character, and the register holds that one.
	s := newWith(t, "abcdef")
	feed(t, s, "3x")
	checkText(t, s, "bcdef")
	if s.register != "a" {
		t.Fatalf("register = %q, want a", s.register)
	}

	// 3w moves one word.
	s = newWith(t, "alpha beta gamma")
	feed(t, s, "3w")
	checkCursor(t, s, 0, 6)

	// 3rX replaces one character.
	s = newWith(t, "abcdef")
	feed(t, s, "3rX")
	checkText(t, s, "Xbcdef")

	// 3p pastes once.
	s = newWith(t, "abc")
	feed(t, s, "yy3p")
	checkText(t, s, "abc\nabc")

	// 3u undoes once.
	s = newWith(t, "abcdef")
	feed(t, s, "xx3u")
	checkText(t, s, "bcdef")
}

// TestNoCountPrefixObjectStaysInnermost: a counted text object has no
// way to reach a level out — "2di(" is the invalid "d" plus the "2",
// and the object that follows is the innermost pair.
func TestNoCountPrefixObjectStaysInnermost(t *testing.T) {
	s := newWith(t, "x(a(bc)d)y")
	feed(t, s, "lllll")
	feed(t, s, "di(")
	checkText(t, s, "x(a()d)y")
}

// TestZeroIsStillTheLineStartMotion: "0" is a motion, never a count, so
// it still moves to the first column.
func TestZeroIsStillTheLineStartMotion(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "l")
	checkCursor(t, s, 0, 1)
	feed(t, s, "0")
	checkCursor(t, s, 0, 0)
}
