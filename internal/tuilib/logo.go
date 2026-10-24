package tuilib

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/branding"
)

// LogoPalette is the exact RGB triple used by each non-transparent
// index in the logo. Index 0 is the transparent sentinel.
var LogoPalette = [][3]byte{
	{0, 0, 0}, // 0 = transparent sentinel

	// row 0 — warm gradient (yellow → orange → red)
	{219, 177, 49}, // 1  warm yellow
	{242, 146, 46}, // 2  orange (fg)
	{246, 145, 46}, // 3  orange (bg)
	{240, 114, 54}, // 4  dark orange (fg)
	{243, 115, 55}, // 5  dark orange (bg)
	{240, 88, 59},  // 6  red

	// row 1 — green band (yellow-green → orange → red)
	{158, 195, 69}, // 7  green (fg)
	{134, 198, 78}, // 8  green (bg)
	{181, 180, 62}, // 9  yellow-green (fg)
	{117, 180, 94}, // 10 yellow-green (bg)
	{226, 153, 61}, // 11 yellow-orange (fg)
	{204, 149, 77}, // 12 yellow-orange (bg)
	{246, 122, 52}, // 13 orange (fg)
	{239, 121, 71}, // 14 orange (bg)
	{248, 106, 53}, // 15 red-orange (fg)
	{225, 102, 82}, // 16 red-orange (bg)
	{239, 84, 66},  // 17 red (fg)
	{225, 79, 89},  // 18 red (bg)

	// row 2 — rainbow gradient (green → teal → blue → purple)
	{124, 194, 81},  // 19 lime (fg)
	{128, 198, 84},  // 20 lime (bg)
	{113, 194, 92},  // 21 green (fg)
	{84, 184, 129},  // 22 green (bg)
	{92, 169, 143},  // 23 teal (fg)
	{64, 151, 222},  // 24 teal/blue (bg)
	{92, 145, 179},  // 25 cyan-blue (top-only)
	{131, 115, 176}, // 26 muted purple (top-only)
	{116, 111, 195}, // 27 indigo (fg)
	{74, 126, 228},  // 28 cobalt (bg)
	{153, 93, 168},  // 29 mauve (fg)
	{112, 110, 206}, // 30 violet (bg)
	{156, 91, 151},  // 31 pink-purple (fg)
	{143, 100, 180}, // 32 pink-purple (bg)

	// row 3 — split: warm-green left, royal-blue right
	{109, 198, 148}, // 33 aqua-green (top-only)
	{97, 195, 125},  // 34 spring green (fg)
	{98, 186, 213},  // 35 spring green (bg)
	{67, 174, 171},  // 36 teal-green (fg)
	{71, 168, 220},  // 37 cerulean (bg)
	{74, 128, 234},  // 38 royal blue (fg)
	{61, 137, 251},  // 39 royal blue (bg)
	{108, 115, 216}, // 40 periwinkle (fg)
	{74, 129, 240},  // 41 blue (bg)
	{101, 121, 225}, // 42 blue (top-only)

	// row 4 — sky-blue accent
	{103, 185, 244}, // 43 sky (top-only)
	{107, 199, 163}, // 44 mint (fg)
	{100, 182, 246}, // 45 blue (bg) — reused on right as fg
	{56, 134, 251},  // 46 bright blue (top-only)
	{72, 129, 244},  // 47 blue (fg)
	{56, 131, 249},  // 48 blue (bg)
	{61, 133, 252},  // 49 blue (top-only)
}

// LogoGrid is a 10×16 raster matrix of color blocks (sub-pixels).
// Height is 10 sub-pixels (5 terminal rows × 2 half-blocks per cell);
// width is 16 sub-pixels (terminal columns 0..15).
type LogoGrid [10][16]byte

// RefLogoPixels is the captured 10×16 sub-pixel raster matrix of the
// reference Google Antigravity CLI logo (160 bytes total).
var RefLogoPixels = LogoGrid{
	0: {7: 2, 8: 4},
	1: {6: 1, 7: 3, 8: 5, 9: 6},
	2: {5: 7, 6: 9, 7: 11, 8: 13, 9: 15, 10: 17},
	3: {5: 8, 6: 10, 7: 12, 8: 14, 9: 16, 10: 18},
	4: {4: 19, 5: 21, 6: 23, 7: 25, 8: 26, 9: 27, 10: 29, 11: 31},
	5: {4: 20, 5: 22, 6: 24, 9: 28, 10: 30, 11: 32},
	6: {4: 34, 5: 36, 10: 38, 11: 40},
	7: {3: 33, 4: 35, 5: 37, 10: 39, 11: 41, 12: 42},
	8: {3: 44, 4: 45, 11: 46, 12: 47},
	9: {2: 43, 3: 45, 12: 48, 13: 49},
}

// PigasusLogoPixels is the canonical Pigasus CLI banner logo: the reference
// logo rotated 180° ("both"), representing the inverted winged delta.
var PigasusLogoPixels = FlipBoth(RefLogoPixels)

// LogoPixels points to PigasusLogoPixels as the primary logo of this CLI.
var LogoPixels = PigasusLogoPixels

// FlipBoth rotates the 2-D raster 180° (FlipHoriz ∘ FlipVert).
func FlipBoth(g LogoGrid) LogoGrid {
	var out LogoGrid
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			out[y][x] = g[9-y][15-x]
		}
	}
	return out
}

// FlipVert reflects the 2-D raster vertically across the horizontal axis.
func FlipVert(g LogoGrid) LogoGrid {
	var out LogoGrid
	for y := 0; y < 10; y++ {
		out[y] = g[9-y]
	}
	return out
}

// FlipHoriz reflects the 2-D raster horizontally across the vertical axis.
func FlipHoriz(g LogoGrid) LogoGrid {
	var out LogoGrid
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			out[y][x] = g[y][15-x]
		}
	}
	return out
}

// ReverseRows swaps terminal rows (r <-> 4-r) while leaving internal sub-pixels intact.
func ReverseRows(g LogoGrid) LogoGrid {
	var out LogoGrid
	for r := 0; r < 5; r++ {
		out[2*r] = g[2*(4-r)]
		out[2*r+1] = g[2*(4-r)+1]
	}
	return out
}

// InvertColors swaps top and bottom sub-pixels within each terminal cell.
func InvertColors(g LogoGrid) LogoGrid {
	var out LogoGrid
	for r := 0; r < 5; r++ {
		out[2*r] = g[2*r+1]
		out[2*r+1] = g[2*r]
	}
	return out
}

// RenderHalfBlock emits the minimal ANSI escape sequence for one terminal cell.
func RenderHalfBlock(b *strings.Builder, top, bot byte) {
	switch {
	case top == 0 && bot == 0:
		b.WriteByte(' ')
	case top != 0 && bot == 0:
		rgb := LogoPalette[top]
		fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm▀\x1b[39m", rgb[0], rgb[1], rgb[2])
	case top == 0 && bot != 0:
		rgb := LogoPalette[bot]
		fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm▄\x1b[39m", rgb[0], rgb[1], rgb[2])
	default:
		tRGB := LogoPalette[top]
		bRGB := LogoPalette[bot]
		fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀\x1b[39m\x1b[49m",
			tRGB[0], tRGB[1], tRGB[2],
			bRGB[0], bRGB[1], bRGB[2])
	}
}

// RenderLogoRow encodes one terminal row r (0..4) of the raster grid as a string.
func RenderLogoRow(g LogoGrid, r int) string {
	var b strings.Builder
	for c := 0; c < 16; c++ {
		RenderHalfBlock(&b, g[2*r][c], g[2*r+1][c])
	}
	return b.String()
}

// RenderGridLines renders all 5 terminal rows of the given raster grid.
func RenderGridLines(g LogoGrid) [5]string {
	var out [5]string
	for r := 0; r < 5; r++ {
		out[r] = RenderLogoRow(g, r)
	}
	return out
}

// LogoBlockGrid is a 5×6 matrix of RGB color blocks.
type LogoBlockGrid [5][6][3]byte

// Subpixels expands the 5×6 color block grid into its underlying 10×12 sub-pixel raster.
func (g LogoBlockGrid) Subpixels() [10][12][3]byte {
	var out [10][12][3]byte
	for r := 0; r < 5; r++ {
		for c := 0; c < 6; c++ {
			val := g[r][c]
			out[2*r][2*c] = val
			out[2*r][2*c+1] = val
			out[2*r+1][2*c] = val
			out[2*r+1][2*c+1] = val
		}
	}
	return out
}

// RefLogoBlockGrid is the reference Antigravity CLI 5×6 color block logo matrix.
var RefLogoBlockGrid = LogoBlockGrid{
	0: {2: {242, 146, 46}, 3: {240, 114, 54}},
	1: {1: {158, 195, 69}, 2: {226, 153, 61}, 3: {246, 122, 52}, 4: {239, 84, 66}},
	2: {1: {124, 194, 81}, 2: {92, 169, 143}, 3: {116, 111, 195}, 4: {153, 93, 168}},
	3: {1: {97, 195, 125}, 4: {74, 128, 234}},
	4: {0: {103, 185, 244}, 1: {100, 182, 246}, 4: {56, 131, 249}, 5: {61, 133, 252}},
}

// FlipBothBlocks rotates the 5×6 color block grid 180°.
func FlipBothBlocks(g LogoBlockGrid) LogoBlockGrid {
	var out LogoBlockGrid
	for r := 0; r < 5; r++ {
		for c := 0; c < 6; c++ {
			out[r][c] = g[4-r][5-c]
		}
	}
	return out
}

// FlipVertBlocks mirrors the 5×6 color block grid vertically.
func FlipVertBlocks(g LogoBlockGrid) LogoBlockGrid {
	var out LogoBlockGrid
	for r := 0; r < 5; r++ {
		out[r] = g[4-r]
	}
	return out
}

// FlipHorizBlocks mirrors the 5×6 color block grid horizontally.
func FlipHorizBlocks(g LogoBlockGrid) LogoBlockGrid {
	var out LogoBlockGrid
	for r := 0; r < 5; r++ {
		for c := 0; c < 6; c++ {
			out[r][c] = g[r][5-c]
		}
	}
	return out
}

// PigasusLogoBlockGrid is the canonical Wagyu/Pigasus 5×6 color block logo.
var PigasusLogoBlockGrid = FlipBothBlocks(RefLogoBlockGrid)

// LogoBlockGridCurrent is the active 5×6 color block grid used by this CLI.
var LogoBlockGridCurrent = PigasusLogoBlockGrid

// RenderBlockLogoRow renders row r (0..4) of the 5×6 color block grid.
func RenderBlockLogoRow(g LogoBlockGrid, r int) string {
	var b strings.Builder
	if r == 0 {
		b.WriteString("\x1b[0m")
	}
	b.WriteString("  ")
	for c := 0; c < 6; c++ {
		rgb := g[r][c]
		if rgb == ([3]byte{0, 0, 0}) {
			b.WriteString("  ")
		} else {
			fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm  \x1b[0m", rgb[0], rgb[1], rgb[2])
		}
	}
	b.WriteString("  ")
	return b.String()
}

// RenderBlockGridLines renders all 5 terminal rows of the given 5×6 color block grid.
func RenderBlockGridLines(g LogoBlockGrid) [5]string {
	var out [5]string
	for r := 0; r < 5; r++ {
		out[r] = RenderBlockLogoRow(g, r)
	}
	return out
}

// LogoStyle determines how the banner logo is rendered.
type LogoStyle int

const (
	LogoStyleDefault LogoStyle = iota // 10×16 sub-pixel raster using half-block Unicode glyphs
	LogoStyleSimple                   // 5×6 color block matrix using full-cell background blocks
	LogoStyleText                     // text-only (suppressed logo art)
)

// DetectLogoStyle determines the logo rendering style based on environment variables
// and terminal auto-detection.
func DetectLogoStyle() LogoStyle {
	val := strings.ToLower(strings.TrimSpace(os.Getenv("AGY_CLI_LOGO_STYLE")))
	if val == "" {
		val = strings.ToLower(strings.TrimSpace(os.Getenv("WAGYU_LOGO_STYLE")))
	}
	switch val {
	case "text", "none", "off":
		return LogoStyleText
	case "simple", "block", "blocks", "5x6":
		return LogoStyleSimple
	case "default", "half", "halfblock", "half-block":
		return LogoStyleDefault
	}

	tp := os.Getenv("TERM_PROGRAM")
	lt := os.Getenv("LC_TERMINAL")
	if tp == "Apple_Terminal" || tp == "iTerm.app" || lt == "iTerm2" {
		return LogoStyleSimple
	}
	return LogoStyleDefault
}

// CurrentLogoGrid returns the active logo raster depending on the active brand.
func CurrentLogoGrid() LogoGrid {
	if branding.Current().FlippedLogo {
		return PigasusLogoPixels
	}
	return RefLogoPixels
}

// CurrentLogoBlockGrid returns the active 5×6 color block grid depending on the active brand.
func CurrentLogoBlockGrid() LogoBlockGrid {
	if branding.Current().FlippedLogo {
		return PigasusLogoBlockGrid
	}
	return RefLogoBlockGrid
}

// RenderLogoLines renders all 5 terminal rows of the canonical logo.
func RenderLogoLines() [5]string {
	switch DetectLogoStyle() {
	case LogoStyleSimple:
		return RenderBlockGridLines(CurrentLogoBlockGrid())
	case LogoStyleText:
		return [5]string{}
	default:
		return RenderGridLines(CurrentLogoGrid())
	}
}

// BannerLogoLines returns the 5 lines of the startup banner logo art as
// they would appear at the left of the banner.
func BannerLogoLines() []string {
	rows := RenderLogoLines()
	return []string{rows[0], rows[1], rows[2], rows[3], rows[4]}
}

// PrintBannerLogo outputs the startup-banner logo art from the matrix to w.
func PrintBannerLogo(w io.Writer) {
	for _, line := range BannerLogoLines() {
		fmt.Fprintln(w, line)
	}
}

// RenderTransformed renders the logo applying a named transform and LogoStyle.
func RenderTransformed(flip string, style LogoStyle) ([]string, error) {
	if style == LogoStyleSimple {
		return renderTransformedBlocks(flip)
	}
	return renderTransformedHalfBlocks(flip)
}

func renderTransformedBlocks(flip string) ([]string, error) {
	var bg LogoBlockGrid
	switch strings.ToLower(flip) {
	case "none", "":
		bg = CurrentLogoBlockGrid()
	case "both":
		bg = PigasusLogoBlockGrid
	case "ref":
		bg = RefLogoBlockGrid
	case "vert":
		bg = FlipVertBlocks(RefLogoBlockGrid)
	case "horiz":
		bg = FlipHorizBlocks(RefLogoBlockGrid)
	default:
		return nil, fmt.Errorf("logo: unknown --flip=%q (want none|both|ref|vert|horiz)", flip)
	}
	lines := RenderBlockGridLines(bg)
	return lines[:], nil
}

func renderTransformedHalfBlocks(flip string) ([]string, error) {
	var g LogoGrid
	switch strings.ToLower(flip) {
	case "none", "":
		g = CurrentLogoGrid()
	case "both":
		g = PigasusLogoPixels
	case "ref":
		g = RefLogoPixels
	case "vert":
		g = FlipVert(RefLogoPixels)
	case "row-rev":
		g = ReverseRows(RefLogoPixels)
	case "horiz":
		g = FlipHoriz(RefLogoPixels)
	case "invert":
		g = InvertColors(RefLogoPixels)
	default:
		return nil, fmt.Errorf("logo: unknown --flip=%q (want none|both|ref|vert|row-rev|horiz|invert)", flip)
	}
	lines := RenderGridLines(g)
	return lines[:], nil
}
