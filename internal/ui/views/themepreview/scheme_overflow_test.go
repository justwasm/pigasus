package themepreview

import (
	"strings"
	"testing"
)

// schemes is the catalogue the reference ships, in its own order.
var schemes = []string{
	"terminal", "light", "solarized light", "colorblind-friendly light",
	"dark", "solarized dark", "colorblind-friendly dark", "tokyo night",
}

// cardColumn returns the column the preview card's left border starts
// at, measured on the first option row.
func cardColumn(t *testing.T, lines []string) int {
	t.Helper()
	if len(lines) < 2 {
		t.Fatalf("preview has %d lines, want at least 2", len(lines))
	}
	plain := stripANSI(lines[1])
	bar := strings.Index(plain, "│")
	if bar < 0 {
		t.Fatalf("option row has no card border: %q", plain)
	}
	return bar
}

// The Color Scheme preview draws a fixed-width card to the right of the
// option column. A constant column width lets a long scheme name run
// over it and shove the card sideways, so the panel spills past its own
// border — the bug this file guards.
//
// 【一手】 the three columns below were read off agy 1.5.18 with the
// scheme in each state.
func TestPreviewColumnMatchesTheReference(t *testing.T) {
	cases := []struct {
		current string
		want    int
	}{
		{"dark", 33},
		{"colorblind-friendly dark", 42},
		{"colorblind-friendly light", 43},
	}
	for _, c := range cases {
		lines := BuildPreviewLines(schemes, 0, c.current, "", "")
		if got := cardColumn(t, lines); got != c.want {
			t.Errorf("current %q: card starts at column %d, want %d", c.current, got, c.want)
		}
	}
}

// Whatever the current value, no option row may reach past the card's
// border. This is the invariant the overflow bug broke.
func TestNoOptionRowOutrunsTheCard(t *testing.T) {
	for _, current := range schemes {
		for cursor := 0; cursor < len(schemes); cursor++ {
			lines := BuildPreviewLines(schemes, cursor, current, "", "")
			col := cardColumn(t, lines)
			for i, line := range lines {
				plain := stripANSI(line)
				bar := strings.Index(plain, "│")
				if bar < 0 {
					continue
				}
				// The text before the border must be padding only.
				if strings.TrimRight(plain[:bar], " ") != strings.TrimRight(plain[:col], " ") {
					continue
				}
				if bar != col {
					t.Errorf("current %q cursor %d line %d: border at %d, want %d: %q",
						current, cursor, i, bar, col, plain)
				}
			}
		}
	}
}

// A short list must still leave the reference's gap, so the card does
// not jump left when the catalogue is trimmed.
func TestPreviewColumnNeverSitsFlush(t *testing.T) {
	lines := BuildPreviewLines([]string{"Dark", "Light"}, 0, "Dark", "", "")
	plain := stripANSI(lines[1])
	bar := strings.Index(plain, "│")
	if bar != minLeftWidth {
		t.Errorf("card starts at column %d, want the floor %d", bar, minLeftWidth)
	}
	opt := plain[:bar]
	if !strings.HasSuffix(strings.TrimRight(opt, " "), "Dark (current)") {
		t.Errorf("option text lost its marker: %q", opt)
	}
	if len(opt)-len(strings.TrimRight(opt, " ")) < 1 {
		t.Errorf("option sits flush against the card: %q", opt)
	}
}
