// artifactpanel_test.go — the /artifact panel's frame.
//
// The layout is the reference's own: the title, the empty state, a
// blank and the legend.

package artifactpanel

import (
	"regexp"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// sgrRe matches the SGR spans the panel's rows carry.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripSGR removes the SGR spans from a row.
func stripSGR(s string) string {
	return sgrRe.ReplaceAllString(s, "")
}

func TestLinesLayout(t *testing.T) {
	lines := Lines()
	want := []string{
		"Artifacts",
		"  No artifacts",
		"",
		"Keyboard: ↑/↓ Navigate  p preview  enter open  ctrl+g open in editor  esc Dismiss",
	}
	if len(lines) != len(want) {
		t.Fatalf("line count: got %d, want %d\n got: %q\nwant: %q", len(lines), len(want), lines, want)
	}
	for i := range want {
		if got := stripSGR(lines[i]); got != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got, want[i])
		}
	}
}

// The empty state is painted in the dim grey, and the title is the
// reference's own bold span.
func TestLinesColours(t *testing.T) {
	lines := Lines()
	if !strings.HasPrefix(lines[0], panelTitleOpen+"Artifacts"+panelTitleClose) {
		t.Errorf("title = %q", lines[0])
	}
	if !strings.Contains(lines[1], palette.DimGrayOpen) {
		t.Errorf("empty state should be the dim gray: %q", lines[1])
	}
	if !strings.HasSuffix(lines[1], palette.DimGrayClose) {
		t.Errorf("empty state must end with the dim gray close: %q", lines[1])
	}
}
