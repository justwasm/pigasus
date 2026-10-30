package wordmove

import "testing"

func TestAltWordHelperDirect(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		col      int
		wantBack int
		wantFwd  int
	}{
		{name: "ascii two words, mid first word", line: "hello world", col: 2, wantBack: 0, wantFwd: 5},
		{name: "ascii trailing whitespace", line: "hello   ", col: 8, wantBack: 0, wantFwd: 8},
		{name: "ascii leading whitespace", line: "   hello", col: 0, wantBack: 0, wantFwd: 8},
		{name: "chinese chars no spaces", line: "你好世界", col: 2, wantBack: 0, wantFwd: 4},
		{name: "chinese with ascii word mixed", line: "你好 world", col: 1, wantBack: 0, wantFwd: 2},
		{name: "punctuation joins the surrounding word", line: "foo,bar", col: 1, wantBack: 0, wantFwd: 7},
		{name: "col at end stays put on Alt+Right", line: "abc", col: 3, wantBack: 0, wantFwd: 3},
		{name: "col at start stays put on Alt+Left", line: "abc def", col: 0, wantBack: 0, wantFwd: 3},
	}
	for _, tc := range cases {
		t.Run("back/"+tc.name, func(t *testing.T) {
			got := BoundaryBack(tc.line, tc.col)
			if got != tc.wantBack {
				t.Fatalf("BoundaryBack(%q,%d)=%d want %d", tc.line, tc.col, got, tc.wantBack)
			}
		})
		t.Run("fwd/"+tc.name, func(t *testing.T) {
			got := BoundaryFwd(tc.line, tc.col)
			if got != tc.wantFwd {
				t.Fatalf("BoundaryFwd(%q,%d)=%d want %d", tc.line, tc.col, got, tc.wantFwd)
			}
		})
	}
}

func TestMoveMultiLine(t *testing.T) {
	text := "hello\nworld"
	r, c, ok := MoveLeft(text, 1, 0)
	if !ok || r != 0 || c != 0 {
		t.Fatalf("MoveLeft across newline: want (0,0,true), got (%d,%d,%v)", r, c, ok)
	}
	r, c, ok = MoveRight(text, 0, 0)
	if !ok || r != 0 || c != 5 {
		t.Fatalf("MoveRight: want (0,5,true), got (%d,%d,%v)", r, c, ok)
	}
}
