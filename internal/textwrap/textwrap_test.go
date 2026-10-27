// textwrap_test.go — the shared word wrap.
package textwrap

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestWords pins the wrap: the rows break on word boundaries, and a
// word that is wider than the budget is emitted whole — the reference
// does not hyphenate it.
func TestWords(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  []string
	}{
		{"", 10, []string{""}},
		{"one", 10, []string{"one"}},
		{"one two three four", 7, []string{"one two", "three", "four"}},
		{"a b c", 1, []string{"a", "b", "c"}},
		{"supercalifragilistic ok", 5, []string{"supercalifragilistic", "ok"}},
		{"xxxxxxx yy", 7, []string{"xxxxxxx", "yy"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			got := Words(tc.in, tc.width)
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Errorf("Words(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
		})
	}
}

// TestWordsKeepsEveryRowInsideTheBudget pins the budget.
func TestWordsKeepsEveryRowInsideTheBudget(t *testing.T) {
	for _, row := range Words("alpha beta gamma delta", 12) {
		if w := ansi.StringWidth(row); w > 12 {
			t.Errorf("row %q is %d cells, want at most 12", row, w)
		}
	}
}

// TestWordsCountsCellsNotBytes pins the measurement: a CJK word is two
// cells per character, so a row of ideographs holds half as many
// characters as a row of ASCII.
func TestWordsCountsCellsNotBytes(t *testing.T) {
	rows := Words("修复 alpha beta", 6)
	if len(rows) < 2 {
		t.Fatalf("Words = %q, want more than one row", rows)
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 6 {
			t.Errorf("row %q is %d cells, want at most 6", row, w)
		}
	}
	if got := strings.Join(rows, " "); got != "修复 alpha beta" {
		t.Errorf("the rows joined = %q, want the input back", got)
	}
}

// TestWordsEmitsAOverlongWordWhole pins the rule that makes the rows
// overflow: a word that does not fit on a row of its own is emitted
// whole. The reference does not hyphenate it and does not cut it.
func TestWordsEmitsAOverlongWordWhole(t *testing.T) {
	rows := Words("修复修复 alpha", 6)
	if len(rows) == 0 || rows[0] != "修复修复" {
		t.Fatalf("Words = %q, want the overlong word whole on the first row", rows)
	}
	if w := ansi.StringWidth(rows[0]); w <= 6 {
		t.Errorf("the first row is %d cells, want it wider than the budget", w)
	}
}

// TestBreak pins the table cell's word wrap: the rows break on word
// boundaries and a token that is wider than the budget is broken at the
// budget's boundary — a shrunk column whose data is one long URL would
// overflow the column otherwise (cycle 218).
func TestBreak(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  []string
	}{
		{"", 10, []string{""}},
		{"one two three four", 7, []string{"one two", "three", "four"}},
		{"https://example.com/a/very/long/path", 10, []string{
			"https://ex", "ample.com/", "a/very/lon", "g/path",
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			got := Break(tc.in, tc.width)
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Errorf("Break(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
		})
	}
}

// TestBreakNormalisesTheWhitespace pins the table cell's own rule: the
// rows break on whitespace and the whitespace is normalised to a single
// space.
func TestBreakNormalisesTheWhitespace(t *testing.T) {
	got := Break("one   two", 20)
	want := []string{"one two"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("Break = %q, want %q", got, want)
	}
}

// TestBreakKeepsEveryRowInsideTheBudget pins the budget.
func TestBreakKeepsEveryRowInsideTheBudget(t *testing.T) {
	for _, row := range Break("alpha beta gamma delta epsilon", 12) {
		if w := ansi.StringWidth(row); w > 12 {
			t.Errorf("row %q is %d cells, want at most 12", row, w)
		}
	}
}
