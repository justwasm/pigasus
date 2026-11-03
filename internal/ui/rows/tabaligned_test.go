// tabaligned_test.go — the tab-separated content block's own layout.
//
// The layout is pinned by its exact bytes: the column width, the two
// spaces of padding, the rune counting and the trailing-space trim are
// all invisible in a rendered comparison, and every one of them is the
// reference's own.

package rows

import (
	"testing"
)

// tabCase is one fixture: a name and the tab-separated text.
type tabCase struct {
	name string
	text string
	want []string
}

// tabFixtures are the fixtures the layout is exercised with.
//
// The set covers the empty text, a text that is only a newline, a
// single-column row, rows with differing column counts, a row with a
// trailing empty column, a cell with a multi-byte rune (an accented
// letter and an arrow), a wide (CJK) rune, a text with a trailing
// newline, a text with blank lines between rows and a row whose last
// cell is empty.
var tabFixtures = []tabCase{
	{
		name: "empty text",
		text: "",
		want: nil,
	},
	{
		name: "only a newline",
		text: "\n",
		want: nil,
	},
	{
		name: "single column",
		text: "hello",
		want: []string{"hello"},
	},
	{
		name: "two columns",
		text: "a\tbb\nccc\td",
		want: []string{"a    bb", "ccc  d"},
	},
	{
		name: "differing column counts",
		text: "a\tb\tc\nd\te\nf",
		want: []string{"a  b  c", "d  e", "f"},
	},
	{
		name: "trailing empty column",
		text: "a\tb\t\nc\td\t",
		want: []string{"a  b", "c  d"},
	},
	{
		name: "multi-byte rune",
		text: "é\t→\nzz\té",
		want: []string{"é   →", "zz  é"},
	},
	{
		name: "wide rune",
		text: "压缩\t对话\nab\tcd",
		want: []string{"压缩  对话", "ab  cd"},
	},
	{
		name: "trailing newline",
		text: "a\tb\n",
		want: []string{"a  b"},
	},
	{
		name: "blank lines between rows",
		text: "only\n\nnewline\n\nrows\n",
		want: []string{"only", "", "newline", "", "rows"},
	},
	{
		name: "empty cells only",
		text: "\t\n\t",
		want: []string{"", ""},
	},
	{
		name: "trailing tab",
		text: "trailing tab\tx\t",
		want: []string{"trailing tab  x"},
	},
}

// TestRows_TabAlignedRows pins the layout of the tab-separated content
// block.
func TestRows_TabAlignedRows(t *testing.T) {
	for _, tc := range tabFixtures {
		t.Run(tc.name, func(t *testing.T) {
			got := TabAlignedRows(tc.text)
			if len(got) != len(tc.want) {
				t.Fatalf("TabAlignedRows(%q) = %q, want %q", tc.text, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("TabAlignedRows(%q)[%d] = %q, want %q", tc.text, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRows_TabAlignedRowsCountsRunes pins that the column width is
// measured in runes and not in bytes: "é" is one column wide and "ab"
// is two, so the accented cell is padded one space wider than the
// ASCII one. Byte counting would give both the same padding.
func TestRows_TabAlignedRowsCountsRunes(t *testing.T) {
	got := TabAlignedRows("é\tx\nab\ty")
	want := []string{"é   x", "ab  y"}
	if len(got) != len(want) {
		t.Fatalf("TabAlignedRows = %q, want %q", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRows_TabAlignedRowsTrimsTrailingSpaces pins that a row never
// carries a trailing space: the last cell is written as it is and the
// row's right side is trimmed.
func TestRows_TabAlignedRowsTrimsTrailingSpaces(t *testing.T) {
	for _, row := range TabAlignedRows("a\tb\tc") {
		if len(row) > 0 && row[len(row)-1] == ' ' {
			t.Errorf("the row %q carries a trailing space", row)
		}
	}
}
