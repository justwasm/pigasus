// rows_test.go — the /diff file view's row rendering.
package diffaction

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/difffold"
)

func TestRowStylerNumbersTheRows(t *testing.T) {
	// A hunk that starts at line 12 of the old file and 14 of the new
	// one: the removed row carries the old file's number, the added row
	// the new file's.
	styler := NewRowStyler("", 80, 20)
	body := []string{
		"@@ -12,3 +14,4 @@",
		" context",
		"-removed",
		"+added",
		"+added too",
	}
	var out []string
	for _, l := range body {
		out = append(out, styler.Style(l))
	}
	// The context row takes the new file's line 14, so the removed row
	// is the old file's line 13 and the added rows are the new file's
	// lines 15 and 16.
	for _, tc := range []struct {
		row  int
		want string
	}{
		{1, "14"},
		{2, "13"},
		{3, "15"},
		{4, "16"},
	} {
		if got := gutterNumber(out[tc.row]); got != tc.want {
			t.Errorf("row %d = %q, want the gutter to carry %s", tc.row, out[tc.row], tc.want)
		}
	}
	if !strings.Contains(out[2], " -  removed") {
		t.Errorf("removed row = %q, want the reference's marker column", out[2])
	}
	if !strings.Contains(out[3], " +  added") {
		t.Errorf("added row = %q, want the reference's marker column", out[3])
	}
}

func TestRowStylerPassesTheFileHeaderThrough(t *testing.T) {
	styler := NewRowStyler("", 80, 0)
	for _, l := range []string{
		"diff --git a/x.txt b/x.txt",
		"index 1234567..89abcde 100644",
		"--- a/x.txt",
		"+++ b/x.txt",
	} {
		if got := styler.Style(l); got != l {
			t.Errorf("Style(%q) = %q, want it unchanged", l, got)
		}
	}
}

func TestRowStylerTintsTheChangedRows(t *testing.T) {
	// 【strings】 the reference's `/diff` file view paints "a background
	// tint on added and removed lines"
	// (internal/cli/testdata/changelog_reference.txt:127).
	styler := NewRowStyler("", 80, 20)
	added := styler.Style("+added")
	removed := styler.Style("-removed")
	context := styler.Style(" context")
	if !strings.Contains(added, "\x1b[48;2;") {
		t.Errorf("added row carries no tint: %q", added)
	}
	if !strings.Contains(removed, "\x1b[48;2;") {
		t.Errorf("removed row carries no tint: %q", removed)
	}
	if strings.Contains(context, "\x1b[48;2;") {
		t.Errorf("context row carries a tint: %q", context)
	}
}

func TestMaxLineNumber(t *testing.T) {
	hunks := []difffold.Hunk{
		{Header: "@@ -1,3 +1,4 @@", Body: []string{" a"}},
		{Header: "@@ -40,7 +42,9 @@", Body: []string{" b"}},
	}
	if got, want := MaxLineNumber(hunks), 50; got != want {
		t.Fatalf("MaxLineNumber = %d, want %d", got, want)
	}
	if got := MaxLineNumber(nil); got != 0 {
		t.Fatalf("MaxLineNumber(nil) = %d, want 0", got)
	}
}

// gutterNumber returns the line number a rendered row's gutter carries.
func gutterNumber(row string) string {
	plain := stripANSI(row)
	if len(plain) < 4 {
		return ""
	}
	head := plain[:4]
	return strings.TrimSpace(head)
}

// stripANSI removes the escape sequences from a rendered row.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
