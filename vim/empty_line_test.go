package vim

import "testing"

// 【一手】 agy 1.4.9, the vendor's own release notes: "commands on an
// empty last line acting on the line above".
//
// A prompt that ends with a newline has an empty last line. The
// commands that act at the cursor run on the line above it — at that
// line's last character, because the empty line is the continuation of
// the text before it.
//
// The empty line itself stays in the buffer: the text the host reads
// back is unchanged, and the insert entries still reach it.

func TestEmptyLastLineXActsOnTheLineAbove(t *testing.T) {
	s := newWith(t, "ab\n")
	s.Row = 1
	s.clampCursor()
	feed(t, s, "x")
	checkText(t, s, "a\n")
	checkCursor(t, s, 0, 0)
}

func TestEmptyLastLineOperatorActsOnTheLineAbove(t *testing.T) {
	s := newWith(t, "ab\n")
	s.Row = 1
	s.clampCursor()
	feed(t, s, "dw")
	checkText(t, s, "a\n")
}

func TestEmptyLastLineReplaceActsOnTheLineAbove(t *testing.T) {
	s := newWith(t, "ab\n")
	s.Row = 1
	s.clampCursor()
	feed(t, s, "rx")
	checkText(t, s, "ax\n")
}

func TestEmptyLastLineKeepsTheInsertEntries(t *testing.T) {
	s := newWith(t, "ab\n")
	s.Row = 1
	s.clampCursor()
	feed(t, s, "iz")
	checkText(t, s, "ab\nz")
}

func TestEmptyLastLineKeepsTheMotions(t *testing.T) {
	s := newWith(t, "ab\n")
	s.Row = 1
	s.clampCursor()
	feed(t, s, "h")
	checkText(t, s, "ab\n")
	checkCursor(t, s, 1, 0)
}

func TestEmptyLineInTheMiddleIsLeftAlone(t *testing.T) {
	s := newWith(t, "ab\n\ncd")
	s.Row = 1
	s.clampCursor()
	feed(t, s, "dw")
	checkText(t, s, "ab\n\ncd")
}
