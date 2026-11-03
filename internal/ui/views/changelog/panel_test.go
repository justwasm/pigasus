// panel_test.go — the /changelog panel's own rows, pinned.
//
// The panel's document has two shapes and the frame under the window
// has one. Both are pinned here: the shape that a terminal of a known
// size gets and the shape everything else gets.

package changelog_test

import (
	"testing"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/palette"
)

// sameRows reports whether two row sets are byte-identical.
func sameRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The visible-row count is the panel's own geometry: a known height
// gives the reference's layout (the title and the chrome take four
// rows), an unknown one the panel's fixed window.
func TestVisibleRows(t *testing.T) {
	cases := []struct {
		height int
		want   int
	}{
		{0, 18},
		{1, 1},
		{2, 1},
		{4, 1},
		{5, 1},
		{40, 36},
		{41, 37},
	}
	for _, c := range cases {
		if got := changelog.VisibleRows(c.height); got != c.want {
			t.Errorf("VisibleRows(%d) = %d, want %d", c.height, got, c.want)
		}
	}
}

// The page size is the panel's own rule: a known terminal height scrolls
// a whole window, an unknown one scrolls a row less — which is the path
// the unit tests take.
func TestPageRows(t *testing.T) {
	cases := []struct {
		visible int
		height  int
		want    int
	}{
		{18, 0, 17},
		{1, 0, 1},
		{36, 40, 36},
		{5, 24, 5},
	}
	for _, c := range cases {
		spec := tuiviewer.Spec{Visible: c.visible}
		if got := changelog.PageRows(spec, c.height); got != c.want {
			t.Errorf("PageRows(visible=%d, height=%d) = %d, want %d",
				c.visible, c.height, got, c.want)
		}
	}
}

// The document has two shapes: the wrapped one for a terminal whose
// width *and* height are known, and the static one for everything else.
// The height decides — a narrow terminal of an unknown height still
// gets the static document.
func TestDocument_Shapes(t *testing.T) {
	static := changelog.Document(0, 0)
	narrow := changelog.Document(40, 0)
	wrapped := changelog.Document(120, 40)

	if !sameRows(narrow, static) {
		t.Error("Document(40, 0) differs from Document(0, 0): the height decides, not the width")
	}
	if sameRows(wrapped, static) {
		t.Error("Document(120, 40) equals the static document: the wrapped shape is missing")
	}
	for _, d := range [][]string{static, narrow, wrapped} {
		if len(d) < 3 {
			t.Fatalf("the document has %d rows, want at least 3", len(d))
		}
		if d[0] != changelog.TitleRow {
			t.Errorf("the first row = %q, want the title %q", d[0], changelog.TitleRow)
		}
		if d[1] != "" {
			t.Errorf("the second row = %q, want a blank", d[1])
		}
	}
}

// With the reference's release notes reachable, the document is the
// styled source (the static shape) and the wrapped source (the live
// shape).
func TestDocument_MatchesItsPrimitives(t *testing.T) {
	src := changelog.Lines()
	if src == nil {
		t.Skip("the reference's release notes are not reachable from this working directory")
	}
	if !sameRows(changelog.Document(0, 0), changelog.Styled(src)) {
		t.Error("the static document is not the styled source")
	}
	if !sameRows(changelog.Document(120, 40), changelog.Wrapped(src, 120)) {
		t.Error("the wrapped document is not the wrapped source")
	}
}

// Without the reference's release notes, the static document is the
// excerpt followed by the remainder of the release notes.
func TestDocument_NoSource(t *testing.T) {
	if changelog.Lines() != nil {
		t.Skip("the reference's release notes are reachable; the no-source shape is not exercised")
	}
	want := append(append([]string{}, changelog.ExcerptRows()...), changelog.BuildRestRows()...)
	if !sameRows(changelog.Document(0, 0), want) {
		t.Error("the static document is not the excerpt followed by the remainder of the release notes")
	}
}

// The frame under the window is the panel's: the counter and the hint.
//
// The bytes are pinned under the reference's own "solarized light"
// scheme — the scheme the palette starts in.
func TestPanelLines(t *testing.T) {
	orig, _ := palette.CurrentScheme()
	t.Cleanup(func() { palette.SetScheme(orig) })
	if !palette.SetScheme("solarized light") {
		t.Fatal("the solarized light scheme is unknown")
	}

	got := changelog.PanelLines([]string{"alpha", "beta", "gamma"}, 1, 3, 3)
	want := []string{
		"alpha",
		"beta",
		"gamma",
		"\x1b[38;2;101;123;131m  (1\u20133 of 3 lines)\x1b[39m",
		"",
		"  \x1b[1m\x1b[38;5;69m\u2191/\u2193\x1b[0m\x1b[38;2;101;123;131m Navigate \u00b7 \x1b[1m\x1b[38;5;69mesc\x1b[0m\x1b[38;2;101;123;131m Cancel\x1b[39m",
	}
	if !sameRows(got, want) {
		t.Errorf("PanelLines() =\n%q\nwant\n%q", got, want)
	}
}

// The frame copies the rows it is given: the host's window slice is
// not the panel's to modify.
func TestPanelLines_CopiesTheRows(t *testing.T) {
	rows := []string{"alpha", "beta"}
	got := changelog.PanelLines(rows, 1, 2, 2)
	got[0] = "changed"
	if rows[0] != "alpha" {
		t.Errorf("the caller's row = %q, want it untouched", rows[0])
	}
}
