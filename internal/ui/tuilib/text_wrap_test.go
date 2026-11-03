// text_wrap_test.go — the wrap and the escape scanner.
//
// Both are charmbracelet/x/ansi's now — the library the reference's own
// binary embeds. The tests pin the behaviour the hand-rolled versions
// had, and the one place where the library is better: a wide character
// is never cut in half.
package tuilib

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestSkipEscape pins the escape scanner: the byte index just past the
// sequence, for every form the scanner reads.
func TestSkipEscape(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"CSI", "\x1b[31mred", 5},
		{"CSI with parameters", "\x1b[38;5;235m", 11},
		{"CSI with a final byte", "\x1b[1;2;3H", 8},
		{"OSC with BEL", "\x1b]0;title\x07after", 10},
		{"OSC with ST", "\x1b]0;title\x1b\\after", 11},
		{"DCS with ST", "\x1bP1;2qdata\x1b\\after", 12},
		{"SOS with ST", "\x1bXfoo\x1b\\after", 7},
		{"PM with ST", "\x1b^foo\x1b\\after", 7},
		{"APC with ST", "\x1b_foo\x1b\\after", 7},
		{"plain ESC", "\x1bMafter", 2},
		{"lone ESC", "\x1b", 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := SkipEscape(tc.in, 0); got != tc.want {
				t.Errorf("SkipEscape(%q, 0) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestSkipEscapeIgnoresNonEscapes pins the "not an escape" answer.
func TestSkipEscapeIgnoresNonEscapes(t *testing.T) {
	if got := SkipEscape("plain", 0); got != 0 {
		t.Errorf("SkipEscape on plain text = %d, want 0", got)
	}
	if got := SkipEscape("plain", 3); got != 3 {
		t.Errorf("SkipEscape past the end = %d, want 3", got)
	}
}

// TestWrapRows pins the wrap: it is a hard wrap — the rows are at most
// width cells, the text survives the wrap, and the space that falls on
// the break stays at the head of the row after it.
func TestWrapRows(t *testing.T) {
	rows := WrapRows("abc def", 3)
	want := []string{"abc", " de", "f"}
	if len(rows) != len(want) {
		t.Fatalf("WrapRows = %q, want %q", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
	if got := strings.Join(rows, ""); got != "abc def" {
		t.Errorf("the rows concatenated = %q, want the input verbatim", got)
	}
}

// TestWrapRowsKeepsWideCharactersWhole pins the one place where the
// library is better than the hand-rolled cell index.
//
// The cell index counted the end-of-string sentinel as a cell, so a
// line of four CJK ideographs (8 cells) measured as 5 cells and was
// handed back whole at a budget of 5 — eight cells in a five-cell row.
// The wrap cuts at the cell boundary and never splits a grapheme
// cluster, so the ideographs stay whole and the row stays inside the
// budget.
func TestWrapRowsKeepsWideCharactersWhole(t *testing.T) {
	rows := WrapRows("修复修复", 5)
	want := []string{"修复", "修复"}
	if len(rows) != len(want) {
		t.Fatalf("WrapRows = %q, want %q", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
	for i, row := range rows {
		if w := ansi.StringWidth(row); w > 5 {
			t.Errorf("row %d is %d cells, want at most 5: %q", i, w, row)
		}
	}
}

// TestWrapRowsKeepsEscapes pins the escape sequences: they stay in the
// row they were in and they do not count toward the budget.
func TestWrapRowsKeepsEscapes(t *testing.T) {
	line := "\x1b[31mabcdef\x1b[0m"
	rows := WrapRows(line, 3)
	if len(rows) != 2 {
		t.Fatalf("WrapRows = %q, want two rows", rows)
	}
	if !strings.Contains(rows[0], "\x1b[31m") {
		t.Errorf("row 0 lost the colour escape: %q", rows[0])
	}
	if got := ansi.StringWidth(rows[0]); got > 3 {
		t.Errorf("row 0 is %d cells, want at most 3: %q", got, rows[0])
	}
}
