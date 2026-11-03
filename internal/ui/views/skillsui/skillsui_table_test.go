// skillsui_table_test.go — the GFM pipe table's cell metrics.
//
// The table measures its columns in terminal cells, not in runes and
// not in bytes: a CJK character is two cells wide, so a column of
// ideographs is twice as wide as its rune count says. The wrap, the
// column width and the padding all agree on that measure.
package skillsui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestMDWrapTableCellBreaksALongToken pins the table's own word wrap: a
// token that is wider than the column is broken at the column's
// boundary — a shrunk column whose data is one long URL would overflow
// the column otherwise (cycle 218).
func TestMDWrapTableCellBreaksALongToken(t *testing.T) {
	got := MDWrapTableCell("https://example.com/a/very/long/path", 10)
	want := []string{"https://ex", "ample.com/", "a/very/lon", "g/path"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("MDWrapTableCell = %q, want %q", got, want)
	}
}

// TestMDWrapTableCellCountsCells pins the measure: a CJK word is two
// cells per character, so a column of four cells holds two ideographs.
func TestMDWrapTableCellCountsCells(t *testing.T) {
	got := MDWrapTableCell("修复修复", 4)
	want := []string{"修复", "修复"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("MDWrapTableCell = %q, want %q", got, want)
	}
}

// TestMDTableColWidthCountsCells pins the column width: the natural
// width of a cell of ideographs is twice its rune count.
func TestMDTableColWidthCountsCells(t *testing.T) {
	if got := mdTableColWidth("修复修复"); got != 8 {
		t.Errorf("mdTableColWidth = %d cells, want 8", got)
	}
	if got := mdTableColWidth(" ab "); got != 2 {
		t.Errorf("mdTableColWidth = %d cells, want 2", got)
	}
}

// TestMDCellRenderPadsToCells pins the padding: the cell is padded with
// spaces to the column's width, counted in cells.
func TestMDCellRenderPadsToCells(t *testing.T) {
	rows := mdCellRender("修复", 8)
	if len(rows) != 1 {
		t.Fatalf("mdCellRender = %q, want one row", rows)
	}
	if got := ansi.StringWidth(rows[0]); got != 8 {
		t.Errorf("the padded row is %d cells, want 8: %q", got, rows[0])
	}
}

// TestMDCellRenderNeverSplitsACharacter pins the truncation: a cell
// that is too wide is cut at the cell boundary, so the cut never lands
// in the middle of a multi-byte character.
func TestMDCellRenderNeverSplitsACharacter(t *testing.T) {
	rows := mdCellRender("修复修复", 3)
	for _, r := range rows {
		if !utf8Valid(r) {
			t.Errorf("the row %q is not valid UTF-8", r)
		}
		if got := ansi.StringWidth(r); got > 3 {
			t.Errorf("the row is %d cells, want at most 3: %q", got, r)
		}
	}
}

// utf8Valid reports whether s is valid UTF-8.
func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
