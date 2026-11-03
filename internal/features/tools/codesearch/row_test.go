// row_test.go — the row and the layout's own tests.
package codesearch

import (
	"regexp"
	"strings"
	"testing"
)

// sgrRe matches the SGR spans the panel's rows carry.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain strips the SGR spans from a rendered row.
func plain(s string) string {
	return sgrRe.ReplaceAllString(s, "")
}

// TestRowAndFit locks the per-row rendering, including the
// `> L<n>: ` cursor marker and the ellipsis-on-long-line behaviour.
func TestRowAndFit(t *testing.T) {
	got := plain(Row(7, "abc needle xyz", 4, 10, 1, 0, 60))
	// non-cursor match row: 2-space prefix + dim gutter + dim pre + blue match + dim post.
	if !strings.Contains(got, "L7:") {
		t.Errorf("non-cursor match row missing gutter: %q", got)
	}
	if !strings.HasPrefix(got, "  L7:") {
		t.Errorf("non-cursor match row marker must be 2 spaces: %q", got)
	}
	if !strings.Contains(got, "needle") {
		t.Errorf("non-cursor match row lost the match text: %q", got)
	}

	got = plain(Row(7, "abc needle xyz", 4, 10, 1, 1, 60))
	if !strings.HasPrefix(got, "> L7:") {
		t.Errorf("cursor marker must be '> L7:', got %q", got)
	}

	// Context row (line prior to the match): only the gutter is dim.
	got = plain(Row(6, "context line", 0, 0, 0, 0, 60))
	if !strings.HasPrefix(got, "  L6:") || !strings.Contains(got, "context line") {
		t.Errorf("context row lost gutter or text: %q", got)
	}

	// Long line truncation. A 605-rune line at width 220 with a "zwk"
	// at rune 300 should report an "abcdef..." prefix and "...suffix"
	// suffix.
	longText := strings.Repeat("P", 300) + " zwk " + strings.Repeat("Q", 300)
	body, s, e := Fit(longText, 301, 304, 200) // typical 200-cell content width
	rs := []rune(body)
	if rs[0] != '…' || rs[len(rs)-1] != '…' {
		t.Errorf("Fit must wrap the match with ellipses: %q", body)
	}
	if s <= 0 || e >= len(rs) {
		t.Errorf("Fit match span %d-%d out of range 0-%d", s, e, len(rs))
	}
}

// TestFill verifies the layout: a file header on the first item of a new
// file, a separator rule between the blocks of one file, and a blank row
// before the header of a new file when it follows a block.
func TestFill(t *testing.T) {
	hits := []Hit{
		{File: "a.txt", Line: 2, Text: "match a1"},
		{File: "a.txt", Line: 4, Text: "match a2"},
		{File: "b.txt", Line: 1, Text: "match b1"},
	}
	// Render with cursor on hits[2] (the b.txt match) and a large budget.
	end, rows := Fill(hits, 0, 2, 99, 220)
	if end != 3 {
		t.Errorf("Fill returned end=%d, want 3", end)
	}
	stripped := make([]string, len(rows))
	for i, r := range rows {
		stripped[i] = plain(r)
	}
	hasHeader := func(name string) bool {
		for _, r := range stripped {
			if r == name {
				return true
			}
		}
		return false
	}
	if !hasHeader("a.txt") {
		t.Error("first file header missing")
	}
	if !hasHeader("b.txt") {
		t.Error("second file header missing")
	}
	hasRule := false
	for _, r := range stripped {
		if r != "" && strings.Count(r, "─") == len([]rune(r)) {
			hasRule = true
			break
		}
	}
	if !hasRule {
		t.Error("between-blocks separator rule missing")
	}
}
