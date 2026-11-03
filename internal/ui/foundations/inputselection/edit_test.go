package inputselection

import "testing"

type cursorCase struct {
	name     string
	val      string
	offset   int
	wantLine int
	wantCol  int
}

var cursorCases = []cursorCase{
	{"the start", "hello", 0, 0, 0},
	{"the middle", "hello", 3, 0, 3},
	{"the end", "hello", 5, 0, 5},
	{"past the end", "hello", 9, 0, 5},
	{"negative", "hello", -1, 0, 0},
	{"an empty value", "", 0, 0, 0},
	{"an empty value, past the end", "", 4, 0, 0},
	{"the second line", "a\nb\nc", 2, 1, 0},
	{"the second line, one byte in", "a\nb\nc", 3, 1, 1},
	{"the third line", "a\nb\nc", 4, 2, 0},
	{"the last line", "a\nb\nc", 5, 2, 1},
	{"a wide rune counts as one column", "日本語", 3, 0, 1},
	{"two wide runes", "日本語", 6, 0, 2},
	{"a trailing newline", "a\n", 2, 1, 0},
	{"a line with spaces", "a\n  b", 5, 1, 3},
}

func TestCursorPosition(t *testing.T) {
	for _, tc := range cursorCases {
		t.Run(tc.name, func(t *testing.T) {
			line, col := CursorPosition(tc.val, tc.offset)
			if line != tc.wantLine || col != tc.wantCol {
				t.Fatalf("CursorPosition(%q, %d) = (%d, %d), want (%d, %d)",
					tc.val, tc.offset, line, col, tc.wantLine, tc.wantCol)
			}
		})
	}
}

type deleteCase struct {
	name   string
	val    string
	start  int
	end    int
	want   string
	wantOK bool
}

var deleteCases = []deleteCase{
	{"the middle", "hello", 1, 3, "hlo", true},
	{"the whole value", "hello", 0, 5, "", true},
	{"an empty range", "hello", 2, 2, "hello", true},
	{"the tail", "hello", 3, 5, "hel", true},
	{"past the end of the value", "hello", 3, 99, "hello", false},
	{"beyond the value", "hello", 9, 12, "hello", false},
	{"a reversed range", "hello", 3, 1, "hlo", true},
	{"across a newline", "a\nb", 2, 3, "a\n", true},
	{"an empty value", "", 0, 0, "", true},
}

func TestDeleteRange(t *testing.T) {
	for _, tc := range deleteCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DeleteRange(tc.val, tc.start, tc.end)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("DeleteRange(%q, %d, %d) = (%q, %v), want (%q, %v)",
					tc.val, tc.start, tc.end, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

type replaceCase struct {
	name   string
	val    string
	start  int
	end    int
	repl   string
	want   string
	wantOK bool
}

var replaceCases = []replaceCase{
	{"the middle", "hello", 1, 3, "XY", "hXYlo", true},
	{"the whole value", "hello", 0, 5, "X", "X", true},
	{"an empty range", "hello", 2, 2, "Z", "heZllo", true},
	{"the tail", "hello", 3, 5, "L", "helL", true},
	{"past the end of the value", "hello", 3, 99, "X", "hello", false},
	{"beyond the value", "hello", 9, 12, "X", "hello", false},
	{"a reversed range", "hello", 3, 1, "R", "hRlo", true},
	{"across a newline", "a\nb", 2, 3, "T", "a\nT", true},
	{"an empty replacement", "hello", 1, 3, "", "hlo", true},
	{"an empty value", "", 0, 0, "E", "E", true},
}

func TestReplaceRange(t *testing.T) {
	for _, tc := range replaceCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ReplaceRange(tc.val, tc.start, tc.end, tc.repl)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("ReplaceRange(%q, %d, %d, %q) = (%q, %v), want (%q, %v)",
					tc.val, tc.start, tc.end, tc.repl, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
