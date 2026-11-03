// panel_test.go — the /feature-diff panel's own rows, pinned.
//
// The panel shows one of two frames: the feature table, or the answer
// that the clone has no table. Both are pinned here, byte for byte.

package feat

import (
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// The frame the panel shows when the clone has no feature table.
//
// The bytes are pinned under the reference's own "solarized light"
// scheme — the scheme the palette starts in.
func TestNotFoundLines(t *testing.T) {
	orig, _ := palette.CurrentScheme()
	t.Cleanup(func() { palette.SetScheme(orig) })
	if !palette.SetScheme("solarized light") {
		t.Fatal("the solarized light scheme is unknown")
	}

	want := []string{
		"  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Feature Diff \x1b[0m",
		"",
		"\x1b[38;2;101;123;131m  data file not found (run from the project root).\x1b[39m",
		"",
		"  \x1b[1m\x1b[38;5;69mesc\x1b[0m\x1b[38;2;101;123;131m Close\x1b[39m",
	}
	got := notFoundLines()
	if len(got) != len(want) {
		t.Fatalf("notFoundLines() has %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// With the feature table reachable, the panel shows the table's own
// document.
func TestPanelLines_ShowsTheTable(t *testing.T) {
	tbl := SourceData()
	if tbl == nil {
		t.Skip("feature_diff_table.json is not reachable from this working directory")
	}
	got := PanelLines()
	want := PanelDoc(tbl)
	if len(got) != len(want) {
		t.Fatalf("PanelLines() has %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// The window over the table: a known height gives the reference's own
// geometry (the title and the chrome take four rows), an unknown one
// the panel's fixed window.
func TestSpec_VisibleRows(t *testing.T) {
	tbl := SourceData()
	if tbl == nil {
		t.Skip("feature_diff_table.json is not reachable from this working directory")
	}
	doc := PanelDoc(tbl)

	cases := []struct {
		height int
		want   int
	}{
		{0, 17},
		{1, 1},
		{4, 1},
		{5, 1},
		{40, 36},
	}
	for _, c := range cases {
		spec := Spec(c.height)
		if spec.Visible != c.want {
			t.Errorf("Spec(%d).Visible = %d, want %d", c.height, spec.Visible, c.want)
		}
		if spec.Total != len(doc) {
			t.Errorf("Spec(%d).Total = %d, want %d", c.height, spec.Total, len(doc))
		}
		if len(spec.Lines) != len(doc) {
			t.Errorf("Spec(%d) has %d rows, want %d", c.height, len(spec.Lines), len(doc))
		}
	}
}

// Without the feature table there is no document to scroll.
func TestSpec_NoTable(t *testing.T) {
	if SourceData() != nil {
		t.Skip("feature_diff_table.json is reachable; the no-table shape is not exercised")
	}
	spec := Spec(40)
	if spec.Visible != 0 || spec.Total != 0 || len(spec.Lines) != 0 {
		t.Errorf("Spec(40) = %+v, want the empty spec", spec)
	}
}
