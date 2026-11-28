package configpanel

// panel_row_width_test.go — a settings row must fit the panel it is drawn in.
//
// # What this pins
//
// A settings row is "  <name><pad><value>" with a fixed name column, so a name
// wider than that column used to push the row past the panel's right edge.
// "colorblind-friendly light" is the widest value in the catalogue: at 40 or 52
// columns the row stayed 56 cells wide and spilled over whatever the panel
// drew beside it — the frame and the preview pane on the right.
//
// The reference does not do this: its rows stay inside the panel at every
// width. The row is now clamped to the width it is drawn in, so the value is
// what gets abbreviated, never the frame.

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// widestRow returns the widest rendered catalogue row at the given width.
func widestRow(t *testing.T, p *Panel, width int) (int, string) {
	t.Helper()
	worst, worstRow := 0, ""
	for _, r := range p.Rows() {
		for _, v := range r.Values {
			for _, cursor := range []bool{false, true} {
				line := p.renderItemRow(r, v.Display, cursor, width)
				if w := tuilib.StringWidth(line); w > worst {
					worst, worstRow = w, r.Name+" = "+v.Display
				}
			}
		}
	}
	return worst, worstRow
}

// TestSettingsRowFitsThePanelWidth pins the clamp at every width the panel can
// be drawn in, including the narrow ones where the long scheme names used to
// overflow.
func TestSettingsRowFitsThePanelWidth(t *testing.T) {
	p := New(Seams{})

	for _, width := range []int{30, 40, 52, 56, 60, 72, 80, 100, 120} {
		worst, row := widestRow(t, p, width)
		if worst > width {
			t.Errorf("at width %d the widest row is %d cells (%s); a row must not exceed its panel",
				width, worst, row)
		}
	}
}

// TestSettingsRowKeepsItsContentWhenItFits pins the other half: the clamp must
// not touch a row that already fits, or the reference's byte layout would
// change on every normal terminal.
func TestSettingsRowKeepsItsContentWhenItFits(t *testing.T) {
	p := New(Seams{})

	var row Row
	var value string
	for _, r := range p.Rows() {
		if r.Name == "Color Scheme" {
			row = r
			for _, v := range r.Values {
				if v.Display == "colorblind-friendly light" {
					value = v.Display
				}
			}
		}
	}
	if value == "" {
		t.Fatal("the catalogue no longer carries colorblind-friendly light")
	}

	// The reference's own rendering of this row, measured from a live capture:
	// "> Color Scheme" + pad + the value, 56 cells.
	const want = "> Color Scheme                 colorblind-friendly light"
	got := tuilib.StringWidth(p.renderItemRow(row, value, true, 80))
	if got != tuilib.StringWidth(want) {
		t.Errorf("row width at 80 columns = %d, want %d (the reference's own layout)", got, tuilib.StringWidth(want))
	}
	if !strings.Contains(tuilib.FitWidth(p.renderItemRow(row, value, true, 80), 80), "colorblind-friendly light") {
		t.Errorf("the value was abbreviated in a panel wide enough to hold it")
	}
}

// TestSettingsRowWithUnknownWidthIsNotBlanked pins the zero-width convention.
//
// A caller with no viewport yet passes width 0. That means "not measured", not
// "no room": clamping to it would blank every row, and the cursor row would
// stop being findable at all (its "> " prefix is how the host finds it).
func TestSettingsRowWithUnknownWidthIsNotBlanked(t *testing.T) {
	p := New(Seams{})

	var row Row
	var value string
	for _, r := range p.Rows() {
		if r.Name == "Color Scheme" {
			row = r
			for _, v := range r.Values {
				if v.Display == "colorblind-friendly light" {
					value = v.Display
				}
			}
		}
	}
	if value == "" {
		t.Fatal("the catalogue no longer carries colorblind-friendly light")
	}

	got := p.renderItemRow(row, value, true, 0)
	if strings.TrimSpace(stripSGR(got)) == "" {
		t.Fatalf("width 0 blanked the row")
	}
	if !strings.HasPrefix(stripSGR(got), "> ") {
		t.Errorf("width 0 lost the cursor marker: %q", stripSGR(got))
	}
}

// stripSGR removes the row's colour spans so the assertions read the text.
func stripSGR(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
