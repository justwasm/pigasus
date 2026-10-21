// logo_palette_test.go — cycle 290: tests for the 10×16 sub-pixel raster
// matrix, palette, reflections, and ANSI terminal rendering.
package main

import (
	"strings"
	"testing"
)

// TestLogoPalette_TransparentSentinel guards the "index 0 means
// transparent" invariant. If somebody ever adds an entry that uses
// pure black, this test catches the conflict.
func TestLogoPalette_TransparentSentinel(t *testing.T) {
	if len(logoPalette) == 0 {
		t.Fatal("logoPalette is empty")
	}
	if logoPalette[0] != ([3]byte{0, 0, 0}) {
		t.Fatalf("logoPalette[0] = %v, want {0,0,0} (the transparent sentinel)",
			logoPalette[0])
	}
	for i, c := range logoPalette {
		if i == 0 {
			continue
		}
		if c[0] == 0 && c[1] == 0 && c[2] == 0 {
			t.Errorf("logoPalette[%d] = {0,0,0} collides with the transparent sentinel", i)
		}
	}
}

// TestLogoPixels_Dimensions pins the 10×16 raster matrix bounds.
func TestLogoPixels_Dimensions(t *testing.T) {
	if len(logoPixels) != 10 {
		t.Fatalf("logoPixels has %d rows, want 10", len(logoPixels))
	}
	for y, row := range logoPixels {
		if len(row) != 16 {
			t.Errorf("row %d has %d columns, want 16", y, len(row))
		}
	}
}

// TestLogoPixels_ActivePixelCount guards that exactly 50 non-zero
// color blocks exist across the 160-cell raster.
func TestLogoPixels_ActivePixelCount(t *testing.T) {
	var count int
	for _, row := range logoPixels {
		for _, pixel := range row {
			if pixel != 0 {
				count++
			}
		}
	}
	if count != 50 {
		t.Fatalf("active pixel count = %d, want 50", count)
	}
}

// TestRenderLogoLine_RoundTripsPixel walks every terminal cell and asserts
// that the rendered ANSI contains a Unicode half-block glyph (▀ or ▄) or
// space matching the sub-pixel values.
func TestRenderLogoLine_RoundTripsPixel(t *testing.T) {
	t.Setenv("AGY_CLI_LOGO_STYLE", "default")
	lines := renderLogoLines()
	for r, line := range lines {
		visible := logoStripAnsi(line)
		visibleRunes := []rune(visible)
		if len(visibleRunes) != 16 {
			t.Fatalf("row %d visible rune count = %d, want 16", r, len(visibleRunes))
		}
		for c := 0; c < 16; c++ {
			top := logoPixels[2*r][c]
			bot := logoPixels[2*r+1][c]

			var want rune = ' '
			switch {
			case top == 0 && bot == 0:
				want = ' '
			case top != 0 && bot == 0:
				want = '▀'
			case top == 0 && bot != 0:
				want = '▄'
			default:
				want = '▀'
			}

			if visibleRunes[c] != want {
				t.Errorf("row %d col %d (top=%d, bot=%d): visible glyph %q, want %q",
					r, c, top, bot, visibleRunes[c], want)
			}
		}
	}
}

// TestFlipVert_VerticalMirror verifies that on the 10×16 sub-pixel raster,
// vertical reflection is literally a 2-D row reversal:
//
//	flipped[y] == orig[9 - y]
func TestFlipVert_VerticalMirror(t *testing.T) {
	flipped := flipVert(logoPixels)
	for y := 0; y < 10; y++ {
		if flipped[y] != logoPixels[9-y] {
			t.Errorf("row %d: flipVert did not match orig row %d", y, 9-y)
		}
	}
}

// TestFlipHoriz_HorizontalMirror verifies that on the 10×16 sub-pixel raster,
// horizontal reflection is literally a column reversal:
//
//	mirrored[y][x] == orig[y][15 - x]
func TestFlipHoriz_HorizontalMirror(t *testing.T) {
	mirrored := flipHoriz(logoPixels)
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			if mirrored[y][x] != logoPixels[y][15-x] {
				t.Errorf("(%d,%d): flipHoriz got %d, want %d",
					y, x, mirrored[y][x], logoPixels[y][15-x])
			}
		}
	}
}

// TestFlipHoriz_PreservesShape verifies that because the logo shape is
// axially symmetric around column 7.5, horizontal reflection preserves
// the visible glyph positions and indentation across all rows.
func TestFlipHoriz_PreservesShape(t *testing.T) {
	mirrored := flipHoriz(logoPixels)
	for r := 0; r < 5; r++ {
		origVis := logoStripAnsi(renderLogoRow(logoPixels, r))
		flipVis := logoStripAnsi(renderLogoRow(mirrored, r))
		if origVis != flipVis {
			t.Errorf("row %d shape deformed under horiz flip:\n orig: %q\n flip: %q",
				r, origVis, flipVis)
		}
	}
}

// TestFlipHoriz_MirrorsColors confirms that while the shape is invariant,
// the color gradient across each row is actively mirrored (i.e. the byte
// streams differ because ANSI color codes are reversed).
func TestFlipHoriz_MirrorsColors(t *testing.T) {
	mirrored := flipHoriz(logoPixels)
	for r := 0; r < 5; r++ {
		origANSI := renderLogoRow(logoPixels, r)
		flipANSI := renderLogoRow(mirrored, r)
		if origANSI == flipANSI {
			t.Errorf("row %d color gradient did not mirror (ANSI bytes identical)", r)
		}
	}
}

// TestReverseRows_TerminalRowSwap verifies that reverseRows swaps terminal
// cell rows without swapping sub-pixels:
//
//	out[2*r] == orig[2*(4-r)]
//	out[2*r+1] == orig[2*(4-r)+1]
func TestReverseRows_TerminalRowSwap(t *testing.T) {
	out := reverseRows(logoPixels)
	for r := 0; r < 5; r++ {
		if out[2*r] != logoPixels[2*(4-r)] || out[2*r+1] != logoPixels[2*(4-r)+1] {
			t.Errorf("cell row %d mismatch under reverseRows", r)
		}
	}
}

// TestInvertColors_SubpixelSwap verifies that invertColors swaps top and
// bottom sub-pixels within each terminal cell.
func TestInvertColors_SubpixelSwap(t *testing.T) {
	out := invertColors(logoPixels)
	for r := 0; r < 5; r++ {
		if out[2*r] != logoPixels[2*r+1] || out[2*r+1] != logoPixels[2*r] {
			t.Errorf("cell row %d mismatch under invertColors", r)
		}
	}
}

// TestFlipVertComposesWithOriginal verifies that stacking original and
// flipVert creates a continuous mirror boundary:
// original's bottom sub-pixel row (y=9) is identical to flipped's top
// sub-pixel row (y=0).
func TestFlipVertComposesWithOriginal(t *testing.T) {
	flipped := flipVert(logoPixels)
	if logoPixels[9] != flipped[0] {
		t.Errorf("boundary mismatch: orig[9]=%v, flipped[0]=%v", logoPixels[9], flipped[0])
	}
}

// logoStripAnsi removes CSI ("ESC[...m") sequences from s.
func logoStripAnsi(s string) string {
	var b strings.Builder
	const esc = 0x1b
	i := 0
	for i < len(s) {
		if s[i] == esc && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) {
				b := s[j]
				if b >= 0x40 && b <= 0x7e {
					j++
					break
				}
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestStripAnsi(t *testing.T) {
	in := "abc\x1b[38;2;1;2;3m▄\x1b[39m  "
	want := "abc▄  "
	if got := logoStripAnsi(in); got != want {
		t.Errorf("logoStripAnsi(%q) = %q, want %q", in, got, want)
	}
}

func TestFlipDemo(t *testing.T) {
	cases := []struct {
		name string
		g    logoGrid
	}{
		{"identity (logoPixels)", logoPixels},
		{"flipVert(logoPixels) — true vertical mirror", flipVert(logoPixels)},
		{"reverseRows(logoPixels) — literal cell row swap, sub-pixels stay", reverseRows(logoPixels)},
		{"flipHoriz(logoPixels)", flipHoriz(logoPixels)},
		{"invertColors(logoPixels)", invertColors(logoPixels)},
	}
	for _, c := range cases {
		t.Logf("── %s ──", c.name)
		for _, row := range renderGridLines(c.g) {
			t.Logf("    %s", row)
		}
	}
}

// TestLogoBlocks_Dimensions guards the 5×6 color block grid bounds.
func TestLogoBlocks_Dimensions(t *testing.T) {
	if len(refLogoBlockGrid) != 5 {
		t.Fatalf("refLogoBlockGrid has %d rows, want 5", len(refLogoBlockGrid))
	}
	for r, row := range refLogoBlockGrid {
		if len(row) != 6 {
			t.Errorf("row %d has %d columns, want 6", r, len(row))
		}
	}
}

// TestLogoBlocks_ActiveBlockCount guards that exactly 16 non-zero color
// blocks exist across the 30 cells of the 5×6 matrix.
func TestLogoBlocks_ActiveBlockCount(t *testing.T) {
	countRef := 0
	for _, row := range refLogoBlockGrid {
		for _, cell := range row {
			if cell != ([3]byte{0, 0, 0}) {
				countRef++
			}
		}
	}
	if countRef != 16 {
		t.Fatalf("ref active block count = %d, want 16", countRef)
	}

	countPigasus := 0
	for _, row := range pigasusLogoBlockGrid {
		for _, cell := range row {
			if cell != ([3]byte{0, 0, 0}) {
				countPigasus++
			}
		}
	}
	if countPigasus != 16 {
		t.Fatalf("pigasus active block count = %d, want 16", countPigasus)
	}
}

// TestFlipBothBlocks_180DegreeFlip asserts that flipBothBlocks
// inverts both axes: r' = 4 - r, c' = 5 - c.
func TestFlipBothBlocks_180DegreeFlip(t *testing.T) {
	for r := 0; r < 5; r++ {
		for c := 0; c < 6; c++ {
			got := pigasusLogoBlockGrid[r][c]
			want := refLogoBlockGrid[4-r][5-c]
			if got != want {
				t.Fatalf("cell (%d,%d) = %v, want %v from ref(%d,%d)",
					r, c, got, want, 4-r, 5-c)
			}
		}
	}
}

// TestLogoBlocks_Subpixel10x12Equivalence pins the 10×12 sub-pixel raster equivalence:
//  1. Every 2×2 "田字方块" (4 sub-pixels) in refLogoBlockGrid.Subpixels() shares identical color.
//  2. 180° rotation on the 10×12 sub-pixel raster (y'=9-y, x'=11-x) is strictly isomorphic
//     to rotating the 5×6 block grid (r'=4-r, c'=5-c) and then expanding to sub-pixels.
func TestLogoBlocks_Subpixel10x12Equivalence(t *testing.T) {
	subpixelsRef := refLogoBlockGrid.Subpixels()

	// 1. Check that every 2×2 "田字方块" has identical color across all 4 sub-pixels
	for r := 0; r < 5; r++ {
		for c := 0; c < 6; c++ {
			expected := refLogoBlockGrid[r][c]
			c00 := subpixelsRef[2*r][2*c]
			c01 := subpixelsRef[2*r][2*c+1]
			c10 := subpixelsRef[2*r+1][2*c]
			c11 := subpixelsRef[2*r+1][2*c+1]

			if c00 != expected || c01 != expected || c10 != expected || c11 != expected {
				t.Fatalf("2×2 block at (%d,%d) has inconsistent sub-pixels: %v, %v, %v, %v (want %v)",
					r, c, c00, c01, c10, c11, expected)
			}
		}
	}

	// 2. Verify 180-degree rotation isomorphism
	subpixelsPigasus := pigasusLogoBlockGrid.Subpixels()
	for y := 0; y < 10; y++ {
		for x := 0; x < 12; x++ {
			got := subpixelsPigasus[y][x]
			want := subpixelsRef[9-y][11-x]
			if got != want {
				t.Fatalf("180° rotation isomorphism failed at sub-pixel (%d,%d): got %v, want %v",
					y, x, got, want)
			}
		}
	}
}

// TestDetectLogoStyle pins the environment variable and terminal detection rules.
func TestDetectLogoStyle(t *testing.T) {
	cases := []struct {
		name       string
		envStyle   string
		wagyuStyle string
		termProg   string
		lcTerm     string
		want       logoStyle
	}{
		{name: "env simple", envStyle: "simple", want: logoStyleSimple},
		{name: "env block", envStyle: "block", want: logoStyleSimple},
		{name: "env 5x6", envStyle: "5x6", want: logoStyleSimple},
		{name: "env default", envStyle: "default", want: logoStyleDefault},
		{name: "env half", envStyle: "half", want: logoStyleDefault},
		{name: "env text", envStyle: "text", want: logoStyleText},
		{name: "wagyu style simple", wagyuStyle: "simple", want: logoStyleSimple},
		{name: "iterm term_program", termProg: "iTerm.app", want: logoStyleSimple},
		{name: "iterm lc_terminal", lcTerm: "iTerm2", want: logoStyleSimple},
		{name: "apple terminal", termProg: "Apple_Terminal", want: logoStyleSimple},
		{name: "xterm default", termProg: "xterm-256color", want: logoStyleDefault},
		{name: "unset default", want: logoStyleDefault},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGY_CLI_LOGO_STYLE", tc.envStyle)
			t.Setenv("WAGYU_LOGO_STYLE", tc.wagyuStyle)
			t.Setenv("TERM_PROGRAM", tc.termProg)
			t.Setenv("LC_TERMINAL", tc.lcTerm)

			if got := detectLogoStyle(); got != tc.want {
				t.Errorf("detectLogoStyle() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRenderBlockLogoLines_WidthAndFormat asserts that the 5×6 color block logo
// renders rows that are visually 16 columns wide and carry 24-bit background sequences.
func TestRenderBlockLogoLines_WidthAndFormat(t *testing.T) {
	lines := renderBlockGridLines(pigasusLogoBlockGrid)
	if len(lines) != 5 {
		t.Fatalf("renderBlockGridLines returned %d lines, want 5", len(lines))
	}

	for r, line := range lines {
		visible := logoStripAnsi(line)
		visibleRunes := []rune(visible)
		if len(visibleRunes) != 16 {
			t.Fatalf("row %d visible rune count = %d, want 16 (visible: %q)",
				r, len(visibleRunes), visible)
		}

		if !strings.Contains(line, "\x1b[48;2;") {
			t.Errorf("row %d expected 24-bit background color escape, got %q", r, line)
		}
	}

	// Row 0 begins with \x1b[0m reset
	if !strings.HasPrefix(lines[0], "\x1b[0m") {
		t.Errorf("row 0 expected \\x1b[0m prefix, got %q", lines[0])
	}
}
