package vim

import "testing"

// 【一手】 agy 1.4.9, the vendor's own release notes: "Fixed Vim mode's
// `r`, `f`, `F`, `t`, and `T` canceling when the target character is a
// space, so `r<space>`, `f<space>`, and `df<space>` now work".
//
// The engine resolves a space target like any other character; this file
// pins the shapes the release note names, so a later change cannot
// cancel the space again.

func TestSpaceTargetReplace(t *testing.T) {
	s := newWith(t, "hello world")
	feed(t, s, "r<space>")
	checkText(t, s, " ello world")
	checkCursor(t, s, 0, 0)
}

func TestSpaceTargetFind(t *testing.T) {
	s := newWith(t, "hello world")
	feed(t, s, "f<space>")
	checkCursor(t, s, 0, 5)

	s = newWith(t, "hello world")
	feed(t, s, "$F<space>")
	checkCursor(t, s, 0, 5)
}

func TestSpaceTargetTill(t *testing.T) {
	s := newWith(t, "hello world")
	feed(t, s, "t<space>")
	checkCursor(t, s, 0, 4)

	s = newWith(t, "hello world")
	feed(t, s, "$T<space>")
	checkCursor(t, s, 0, 6)
}

func TestSpaceTargetOperator(t *testing.T) {
	s := newWith(t, "hello world")
	feed(t, s, "df<space>")
	checkText(t, s, "world")

	s = newWith(t, "hello world")
	feed(t, s, "dt<space>")
	checkText(t, s, "o world")
}
