package tuilib

import (
	"strings"
	"testing"
)

func TestLogoPalette_TransparentSentinel(t *testing.T) {
	if len(LogoPalette) == 0 {
		t.Fatal("LogoPalette is empty")
	}
	if LogoPalette[0] != ([3]byte{0, 0, 0}) {
		t.Fatalf("LogoPalette[0] = %v, want {0,0,0} (the transparent sentinel)", LogoPalette[0])
	}
	for i, c := range LogoPalette {
		if i == 0 {
			continue
		}
		if c[0] == 0 && c[1] == 0 && c[2] == 0 {
			t.Errorf("LogoPalette[%d] = {0,0,0} collides with the transparent sentinel", i)
		}
	}
}

func TestLogoPixels_Dimensions(t *testing.T) {
	if len(LogoPixels) != 10 {
		t.Fatalf("LogoPixels has %d rows, want 10", len(LogoPixels))
	}
	for y, row := range LogoPixels {
		if len(row) != 16 {
			t.Errorf("row %d has %d columns, want 16", y, len(row))
		}
	}
}

func TestLogoPixels_ActivePixelCount(t *testing.T) {
	var count int
	for _, row := range LogoPixels {
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

func TestRenderLogoLine_RoundTripsPixel(t *testing.T) {
	t.Setenv("AGY_CLI_LOGO_STYLE", "default")
	lines := RenderLogoLines()
	for r, line := range lines {
		visible := logoStripAnsi(line)
		visibleRunes := []rune(visible)
		if len(visibleRunes) != 16 {
			t.Fatalf("row %d visible rune count = %d, want 16", r, len(visibleRunes))
		}
		for c := 0; c < 16; c++ {
			top := LogoPixels[2*r][c]
			bot := LogoPixels[2*r+1][c]

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

func TestFlipVert_VerticalMirror(t *testing.T) {
	flipped := FlipVert(LogoPixels)
	for y := 0; y < 10; y++ {
		if flipped[y] != LogoPixels[9-y] {
			t.Errorf("row %d: FlipVert did not match orig row %d", y, 9-y)
		}
	}
}

func TestFlipHoriz_HorizontalMirror(t *testing.T) {
	mirrored := FlipHoriz(LogoPixels)
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			if mirrored[y][x] != LogoPixels[y][15-x] {
				t.Errorf("(%d,%d): FlipHoriz got %d, want %d",
					y, x, mirrored[y][x], LogoPixels[y][15-x])
			}
		}
	}
}

func TestFlipHoriz_PreservesShape(t *testing.T) {
	mirrored := FlipHoriz(LogoPixels)
	for r := 0; r < 5; r++ {
		origVis := logoStripAnsi(RenderLogoRow(LogoPixels, r))
		flipVis := logoStripAnsi(RenderLogoRow(mirrored, r))
		if origVis != flipVis {
			t.Errorf("row %d shape deformed under horiz flip:\n orig: %q\n flip: %q",
				r, origVis, flipVis)
		}
	}
}

func TestFlipHoriz_MirrorsColors(t *testing.T) {
	mirrored := FlipHoriz(LogoPixels)
	for r := 0; r < 5; r++ {
		origANSI := RenderLogoRow(LogoPixels, r)
		flipANSI := RenderLogoRow(mirrored, r)
		if origANSI == flipANSI {
			t.Errorf("row %d color gradient did not mirror (ANSI bytes identical)", r)
		}
	}
}

func TestReverseRows_TerminalRowSwap(t *testing.T) {
	out := ReverseRows(LogoPixels)
	for r := 0; r < 5; r++ {
		if out[2*r] != LogoPixels[2*(4-r)] || out[2*r+1] != LogoPixels[2*(4-r)+1] {
			t.Errorf("cell row %d mismatch under ReverseRows", r)
		}
	}
}

func TestInvertColors_SubpixelSwap(t *testing.T) {
	out := InvertColors(LogoPixels)
	for r := 0; r < 5; r++ {
		if out[2*r] != LogoPixels[2*r+1] || out[2*r+1] != LogoPixels[2*r] {
			t.Errorf("cell row %d mismatch under InvertColors", r)
		}
	}
}

func TestFlipVertComposesWithOriginal(t *testing.T) {
	flipped := FlipVert(LogoPixels)
	if LogoPixels[9] != flipped[0] {
		t.Errorf("boundary mismatch: orig[9]=%v, flipped[0]=%v", LogoPixels[9], flipped[0])
	}
}

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

func TestLogoBlocks_Dimensions(t *testing.T) {
	if len(RefLogoBlockGrid) != 5 {
		t.Fatalf("RefLogoBlockGrid has %d rows, want 5", len(RefLogoBlockGrid))
	}
	for r, row := range RefLogoBlockGrid {
		if len(row) != 6 {
			t.Errorf("row %d has %d columns, want 6", r, len(row))
		}
	}
}

func TestLogoBlocks_ActiveBlockCount(t *testing.T) {
	countRef := 0
	for _, row := range RefLogoBlockGrid {
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
	for _, row := range PigasusLogoBlockGrid {
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

func TestFlipBothBlocks_180DegreeFlip(t *testing.T) {
	for r := 0; r < 5; r++ {
		for c := 0; c < 6; c++ {
			got := PigasusLogoBlockGrid[r][c]
			want := RefLogoBlockGrid[4-r][5-c]
			if got != want {
				t.Fatalf("cell (%d,%d) = %v, want %v from ref(%d,%d)",
					r, c, got, want, 4-r, 5-c)
			}
		}
	}
}

func TestLogoBlocks_Subpixel10x12Equivalence(t *testing.T) {
	subpixelsRef := RefLogoBlockGrid.Subpixels()
	for r := 0; r < 5; r++ {
		for c := 0; c < 6; c++ {
			expected := RefLogoBlockGrid[r][c]
			c00 := subpixelsRef[2*r][2*c]
			c01 := subpixelsRef[2*r][2*c+1]
			c10 := subpixelsRef[2*r+1][2*c]
			c11 := subpixelsRef[2*r+1][2*c+1]

			if c00 != expected || c01 != expected || c10 != expected || c11 != expected {
				t.Fatalf("2×2 block at (%d,%d) has inconsistent sub-pixels", r, c)
			}
		}
	}

	subpixelsPigasus := PigasusLogoBlockGrid.Subpixels()
	for y := 0; y < 10; y++ {
		for x := 0; x < 12; x++ {
			got := subpixelsPigasus[y][x]
			want := subpixelsRef[9-y][11-x]
			if got != want {
				t.Fatalf("180° rotation isomorphism failed at sub-pixel (%d,%d)", y, x)
			}
		}
	}
}

func TestDetectLogoStyle(t *testing.T) {
	cases := []struct {
		name       string
		envStyle   string
		wagyuStyle string
		termProg   string
		lcTerm     string
		want       LogoStyle
	}{
		{name: "env simple", envStyle: "simple", want: LogoStyleSimple},
		{name: "env block", envStyle: "block", want: LogoStyleSimple},
		{name: "env 5x6", envStyle: "5x6", want: LogoStyleSimple},
		{name: "env default", envStyle: "default", want: LogoStyleDefault},
		{name: "env half", envStyle: "half", want: LogoStyleDefault},
		{name: "env text", envStyle: "text", want: LogoStyleText},
		{name: "wagyu style simple", wagyuStyle: "simple", want: LogoStyleSimple},
		{name: "iterm term_program", termProg: "iTerm.app", want: LogoStyleSimple},
		{name: "iterm lc_terminal", lcTerm: "iTerm2", want: LogoStyleSimple},
		{name: "apple terminal", termProg: "Apple_Terminal", want: LogoStyleSimple},
		{name: "xterm default", termProg: "xterm-256color", want: LogoStyleDefault},
		{name: "unset default", want: LogoStyleDefault},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGY_CLI_LOGO_STYLE", tc.envStyle)
			t.Setenv("WAGYU_LOGO_STYLE", tc.wagyuStyle)
			t.Setenv("TERM_PROGRAM", tc.termProg)
			t.Setenv("LC_TERMINAL", tc.lcTerm)

			if got := DetectLogoStyle(); got != tc.want {
				t.Errorf("DetectLogoStyle() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRenderBlockLogoLines_WidthAndFormat(t *testing.T) {
	lines := RenderBlockGridLines(PigasusLogoBlockGrid)
	if len(lines) != 5 {
		t.Fatalf("RenderBlockGridLines returned %d lines, want 5", len(lines))
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

	if !strings.HasPrefix(lines[0], "\x1b[0m") {
		t.Errorf("row 0 expected \\x1b[0m prefix, got %q", lines[0])
	}
}
