// static_test.go — the /usage panel's height-unknown frame, pinned.
//
// The panel has two shapes: the height-aware one and the cycle-193
// fallback the unit tests render. The fallback's window and the legend
// under it are pinned here, byte for byte.

package usagepanel

import (
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// The static document's window: the reference's own figures — the
// document is twenty logical lines and the window shows seventeen.
func TestStaticSpec(t *testing.T) {
	spec := StaticSpec()
	if spec.Visible != 17 {
		t.Errorf("StaticSpec().Visible = %d, want 17", spec.Visible)
	}
	if spec.Total != 20 {
		t.Errorf("StaticSpec().Total = %d, want 20", spec.Total)
	}
	if len(spec.Lines) != len(ViewerLinesStatic()) {
		t.Errorf("StaticSpec() has %d rows, want %d", len(spec.Lines), len(ViewerLinesStatic()))
	}
}

// The frame under the static document: the window's rows, a blank, the
// counter, a blank and the legend.
//
// The bytes are pinned under the reference's own "solarized light"
// scheme — the scheme the palette starts in.
func TestStaticLines(t *testing.T) {
	orig, _ := palette.CurrentScheme()
	t.Cleanup(func() { palette.SetScheme(orig) })
	if !palette.SetScheme("solarized light") {
		t.Fatal("the solarized light scheme is unknown")
	}

	got := StaticLines([]string{"alpha", "beta"}, 1, 2, 20)
	want := []string{
		"alpha",
		"beta",
		"",
		"\x1b[38;2;101;123;131m  (1\u20132 of 20 lines)\x1b[39m",
		"",
		"  \x1b[1m\x1b[38;5;69m\u2191/\u2193\x1b[0m\x1b[38;2;101;123;131m Scroll \u00b7 \x1b[1m\x1b[38;5;69mpgup/pgdown\x1b[0m\x1b[38;2;101;123;131m Page \u00b7 \x1b[1m\x1b[38;5;69mctrl+end\x1b[0m\x1b[38;2;101;123;131m Bottom \u00b7 \x1b[1m\x1b[38;5;69mctrl+home\x1b[0m\x1b[38;2;101;123;131m Top \u00b7 \x1b[1m\x1b[38;5;69mesc\x1b[0m\x1b[38;2;101;123;131m Close\x1b[39m",
	}
	if len(got) != len(want) {
		t.Fatalf("StaticLines() has %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// The frame copies the rows it is given: the host's window slice is
// not the panel's to modify.
func TestStaticLines_CopiesTheRows(t *testing.T) {
	rows := []string{"alpha", "beta"}
	got := StaticLines(rows, 1, 2, 20)
	got[0] = "changed"
	if rows[0] != "alpha" {
		t.Errorf("the caller's row = %q, want it untouched", rows[0])
	}
}
