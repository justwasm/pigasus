package inputselection

import "testing"

const (
	open  = "\x1b[0m\x1b[7m"
	close = "\x1b[0m"
)

type spanCase struct {
	name       string
	val        string
	targetLine int
	wantStart  int
	wantEnd    int
}

var spanCases = []spanCase{
	{"one line", "one line", 0, 0, 8},
	{"empty value", "", 0, 0, 0},
	{"three lines, first", "a\nb\nc", 0, 0, 1},
	{"three lines, second", "a\nb\nc", 1, 2, 3},
	{"three lines, third", "a\nb\nc", 2, 4, 5},
	{"three lines, past the end", "a\nb\nc", 3, 4, 5},
	{"three lines, far past the end", "a\nb\nc", 99, 4, 5},
	{"three lines, negative", "a\nb\nc", -1, 4, 5},
	{"six lines", "l0\nl1\nl2\nl3\nl4\nl5", 5, 15, 17},
	{"six lines, past the end", "l0\nl1\nl2\nl3\nl4\nl5", 6, 15, 17},
	{"empty line in the middle", "a\n\nb", 1, 2, 2},
	{"empty line, last", "a\n\n", 2, 3, 3},
	{"trailing newline", "a\nb\n", 1, 2, 3},
	{"trailing newline, empty last line", "a\nb\n", 2, 4, 4},
}

func TestFindLineSpan(t *testing.T) {
	for _, tc := range spanCases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := FindLineSpan(tc.val, tc.targetLine)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("FindLineSpan(%q, %d) = (%d, %d), want (%d, %d)",
					tc.val, tc.targetLine, start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

type byteOffsetCase struct {
	name      string
	lineText  string
	targetCol int
	want      int
}

var byteOffsetCases = []byteOffsetCase{
	{"ascii, first column", "abc", 0, 0},
	{"ascii, second column", "abc", 1, 1},
	{"ascii, third column", "abc", 2, 2},
	{"ascii, past the end", "abc", 3, 3},
	{"ascii, far past the end", "abc", 99, 3},
	{"ascii, negative", "abc", -1, 0},
	{"wide rune, first column", "日本語", 0, 0},
	{"wide rune, first half", "日本語", 1, 0},
	{"wide rune, second column", "日本語", 2, 3},
	{"wide rune, third column", "日本語", 3, 3},
	{"wide rune, fourth column", "日本語", 4, 6},
	{"wide rune, past the end", "日本語", 6, 9},
	{"emoji, first half", "\U0001F600", 1, 0},
	{"emoji, second half", "\U0001F600", 2, 4},
	{"combining mark, first column", "e\u0301", 0, 0},
	{"combining mark, second column", "e\u0301", 1, 3},
	{"precomposed accent, first column", "\u00e9", 0, 0},
	{"precomposed accent, second column", "\u00e9", 1, 2},
}

func TestByteOffsetInLine(t *testing.T) {
	for _, tc := range byteOffsetCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ByteOffsetInLine(tc.lineText, tc.targetCol); got != tc.want {
				t.Fatalf("ByteOffsetInLine(%q, %d) = %d, want %d", tc.lineText, tc.targetCol, got, tc.want)
			}
		})
	}
}

type offsetCase struct {
	name          string
	value         string
	lineCount     int
	promptScreenY int
	x             int
	y             int
	want          int
}

var offsetCases = []offsetCase{
	{"empty value", "", 1, 10, 4, 10, 0},
	{"first cell of the first line", "hello", 1, 10, 2, 10, 0},
	{"second cell of the first line", "hello", 1, 10, 3, 10, 1},
	{"past the end of the first line", "hello", 1, 10, 40, 10, 5},
	{"a row above the composer", "hello", 1, 10, 4, 9, 0},
	{"a row below the composer", "hello", 1, 10, 4, 11, 5},
	{"the second line", "a\nb", 2, 10, 2, 11, 2},
	{"the second line, one cell in", "a\nb", 2, 10, 3, 11, 3},
	{"the second line, past the end", "a\nb", 2, 10, 9, 11, 3},
}

func TestOffsetAt(t *testing.T) {
	for _, tc := range offsetCases {
		t.Run(tc.name, func(t *testing.T) {
			got := OffsetAt(tc.value, tc.lineCount, tc.promptScreenY, tc.x, tc.y)
			if got != tc.want {
				t.Fatalf("OffsetAt(%q, %d, %d, %d, %d) = %d, want %d",
					tc.value, tc.lineCount, tc.promptScreenY, tc.x, tc.y, got, tc.want)
			}
		})
	}
}

type highlightCase struct {
	name      string
	line      string
	val       string
	lineStart int
	lineEnd   int
	start     int
	end       int
	want      string
}

var highlightCases = []highlightCase{
	{"the whole line", "hello", "hello", 0, 5, 0, 5, "he" + open + "llo" + close},
	{"the middle of the line", "hello", "hello", 0, 5, 1, 3, "hel" + open + "lo" + close},
	{"an empty selection", "hello", "hello", 0, 5, 2, 2, "hello"},
	{"a selection past the end of the line", "hello", "hello", 0, 5, 0, 99, "he" + open + "llo" + close},
	{"a selection that starts before the line", "hello", "hello", 0, 5, -3, 2, "he" + open + "ll" + close + "o"},
	{"a reversed selection", "hello", "hello", 0, 5, 3, 1, "hello"},
	{"a second line of the value", "bcd", "a\nbcd", 2, 5, 2, 5, "bc" + open + "d" + close},
	{"a row the selection does not reach", "bcd", "a\nbcd", 2, 5, 0, 1, "bcd"},
	{"a row that carries the prompt", "> hello", "hello", 0, 5, 0, 5, "> " + open + "hello" + close},
	{"a prompt row, a selection in the middle", "> hello", "hello", 0, 5, 1, 3, "> h" + open + "el" + close + "lo"},
	{"an empty row", "", "hello", 0, 5, 0, 5, ""},
}

func TestHighlightSingleLine(t *testing.T) {
	for _, tc := range highlightCases {
		t.Run(tc.name, func(t *testing.T) {
			got := HighlightSingleLine(tc.line, tc.val, tc.lineStart, tc.lineEnd, tc.start, tc.end)
			if got != tc.want {
				t.Fatalf("HighlightSingleLine(%q, %q, %d, %d, %d, %d) = %q, want %q",
					tc.line, tc.val, tc.lineStart, tc.lineEnd, tc.start, tc.end, got, tc.want)
			}
		})
	}
}

type applyCase struct {
	name      string
	inputView string
	val       string
	start     int
	end       int
	want      string
}

var applyCases = []applyCase{
	{"a selection in the middle", "hello", "hello", 1, 3, "hel" + open + "lo" + close},
	{"a selection covering everything", "hello", "hello", 0, 5, "he" + open + "llo" + close},
	{"an empty selection", "hello", "hello", 2, 2, "hello"},
	{"a reversed selection", "hello", "hello", 3, 1, "hel" + open + "lo" + close},
	{"a selection past the end of the value", "hello", "hello", 0, 99, "he" + open + "llo" + close},
	{"a selection past the end of the value, from the middle", "hello", "hello", 3, 99, "hello"},
	{"a selection beyond the value", "hello", "hello", 9, 12, "hello"},
	{"a row that carries the prompt", "> hello", "hello", 0, 5, "> " + open + "hello" + close},
	{"a multi-line value", "aaa\nbbb", "aaa\nbbb", 1, 5, "aaa\nbb" + open + "b" + close},
	{"a multi-line value with a trailing newline", "aaa\nbbb\n", "aaa\nbbb\n", 1, 5, "aaa\nbb" + open + "b" + close + "\n"},
	{"a multi-line value with the prompt in every row", "> aaa\n  bbb", "aaa\nbbb", 1, 5, "> a" + open + "aa" + close + "\n  " + open + "b" + close + "bb"},
	{"a multi-line value with the prompt and a trailing newline", "> aaa\n  bbb\n", "aaa\nbbb\n", 1, 5, "> a" + open + "aa" + close + "\n  " + open + "b" + close + "bb\n"},
	{"the composer's own escape sequences", "\x1b[38;5;69m>\x1b[m hello", "hello", 1, 3, "\x1b[38;5;69m>\x1b[m h" + open + "el" + close + "\x1b[38;5;69m\x1b[mlo"},
}

func TestApplyHighlight(t *testing.T) {
	for _, tc := range applyCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ApplyHighlight(tc.inputView, tc.val, tc.start, tc.end)
			if got != tc.want {
				t.Fatalf("ApplyHighlight(%q, %q, %d, %d) = %q, want %q",
					tc.inputView, tc.val, tc.start, tc.end, got, tc.want)
			}
		})
	}
}
