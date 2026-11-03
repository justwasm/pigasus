// mcppanel_test.go — the /mcp panel's frame.
//
// The layout is the reference's own, verbatim from
// /tmp/agycmp/panels/ppref.mcp.raw.txt (SGR stripped). Cycle 205
// deliberately left the empty frame as it was, so the rows are pinned
// here rather than only in the host.

package mcppanel

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
		"MCP Servers",
		"",
		"  No MCP servers configured.",
		"",
		"Keyboard: ↑/↓ Navigate  enter Actions  esc Exit",
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

// The empty-state line uses the secondary grey (#586E75), the legend's
// labels the dim grey (#657B83) — both Solarized, cycle 283.
func TestLinesColours(t *testing.T) {
	lines := Lines()
	if !strings.Contains(lines[2], "38;2;88;110;117m") {
		t.Errorf("empty state should be the subtle grey: %q", lines[2])
	}
	if !strings.Contains(lines[4], palette.DimGrayOpen) {
		t.Errorf("legend labels should be the dim gray: %q", lines[4])
	}
	if !strings.HasSuffix(lines[4], palette.DimGrayClose) {
		t.Errorf("legend must end with the label close: %q", lines[4])
	}
}
