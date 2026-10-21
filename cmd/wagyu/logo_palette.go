// logo_palette.go — cycle 290: the banner logo, repainted as a
// 10×16 sub-pixel raster matrix with an indexed color palette.
//
// What this replaces
// ------------------
// Two strings of inline ANSI escapes were carved into `tui_run.go`
// before this file existed:
//
//     var logoBlock = []string{ ... }
//     var logoLines = []string{ ... }
//
// A single string captured two pieces of information per cell — the
// glyph (▀/▄/space) and the (fg, bg) RGB triples — and the relationship
// was only visible by squinting at the raw bytes.
//
// Data Architecture
// -----------------
// A terminal character cell is vertically divided into two halves
// using Unicode half-block glyphs: ▀ (upper half) and ▄ (lower half).
//
// Therefore, the true physical minimum unit is NOT the composite character
// cell, but the SQUARE COLOR BLOCK (the sub-pixel):
//   - Horizontal: 16 columns (indices 0..15), centered at column 7.5.
//   - Vertical: 5 terminal cell rows × 2 halves = 10 sub-pixel rows (0..9).
//
// By representing the logo directly as a 10×16 raster matrix of palette
// indices (`logoGrid`), 2D geometric operations become standard pixel
// coordinate reflections without any ad-hoc top/bot half swapping hacks:
//   - Vertical flip:   y' = 9 - y   (pure row reversal!)
//   - Horizontal flip: x' = 15 - x  (pure column reversal!)
//   - 180° rotation:   y' = 9 - y, x' = 15 - x
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/branding"
)

// logoPalette is the exact RGB triple used by each non-transparent
// index in the logo. Index 0 is the transparent sentinel. Indices are
// grouped by row so the gradient shows up in the index numbers
// themselves (row 0 warms: 1→6; row 2 sweeps the spectrum: 19…32).
var logoPalette = [][3]byte{
	{0, 0, 0}, //  0 = transparent sentinel (no real entry is pure black)

	// row 0 — warm gradient (yellow → orange → red).
	{219, 177, 49}, //  1  warm yellow
	{242, 146, 46}, //  2  orange (fg)
	{246, 145, 46}, //  3  orange (bg)
	{240, 114, 54}, //  4  dark orange (fg)
	{243, 115, 55}, //  5  dark orange (bg)
	{240, 88, 59},  //  6  red

	// row 1 — green band (yellow-green → orange → red).
	{158, 195, 69}, //  7  green (fg)
	{134, 198, 78}, //  8  green (bg)
	{181, 180, 62}, //  9  yellow-green (fg)
	{117, 180, 94}, // 10  yellow-green (bg)
	{226, 153, 61}, // 11  yellow-orange (fg)
	{204, 149, 77}, // 12  yellow-orange (bg)
	{246, 122, 52}, // 13  orange (fg)
	{239, 121, 71}, // 14  orange (bg)
	{248, 106, 53}, // 15  red-orange (fg)
	{225, 102, 82}, // 16  red-orange (bg)
	{239, 84, 66},  // 17  red (fg)
	{225, 79, 89},  // 18  red (bg)

	// row 2 — rainbow gradient (green → teal → blue → purple).
	{124, 194, 81},  // 19  lime (fg)
	{128, 198, 84},  // 20  lime (bg)
	{113, 194, 92},  // 21  green (fg)
	{84, 184, 129},  // 22  green (bg)
	{92, 169, 143},  // 23  teal (fg)
	{64, 151, 222},  // 24  teal/blue (bg)
	{92, 145, 179},  // 25  cyan-blue (top-only)
	{131, 115, 176}, // 26  muted purple (top-only)
	{116, 111, 195}, // 27  indigo (fg)
	{74, 126, 228},  // 28  cobalt (bg)
	{153, 93, 168},  // 29  mauve (fg)
	{112, 110, 206}, // 30  violet (bg)
	{156, 91, 151},  // 31  pink-purple (fg)
	{143, 100, 180}, // 32  pink-purple (bg)

	// row 3 — split: warm-green left, royal-blue right.
	{109, 198, 148}, // 33  aqua-green (top-only)
	{97, 195, 125},  // 34  spring green (fg)
	{98, 186, 213},  // 35  spring green (bg)
	{67, 174, 171},  // 36  teal-green (fg)
	{71, 168, 220},  // 37  cerulean (bg)
	{74, 128, 234},  // 38  royal blue (fg)
	{61, 137, 251},  // 39  royal blue (bg)
	{108, 115, 216}, // 40  periwinkle (fg)
	{74, 129, 240},  // 41  blue (bg)
	{101, 121, 225}, // 42  blue (top-only)

	// row 4 — sky-blue accent.
	{103, 185, 244}, // 43  sky (top-only)
	{107, 199, 163}, // 44  mint (fg)
	{100, 182, 246}, // 45  blue (bg) — reused on the right half as fg
	{56, 134, 251},  // 46  bright blue (top-only)
	{72, 129, 244},  // 47  blue (fg)
	{56, 131, 249},  // 48  blue (bg)
	{61, 133, 252},  // 49  blue (top-only)
}

// logoGrid is a 10×16 raster matrix of color blocks (sub-pixels).
// Height is 10 sub-pixels (5 terminal rows × 2 half-blocks per cell);
// width is 16 sub-pixels (terminal columns 0..15).
//
// Each byte is a 1-based index into logoPalette. 0 is transparent/blank.
type logoGrid [10][16]byte

// refLogoPixels is the captured 10×16 sub-pixel raster matrix of the
// reference Google Antigravity CLI logo (160 bytes total).
// Every sub-pixel row is axially symmetric around column 7.5.
// Unlisted elements in the sparse literal default to 0 (transparent).
var refLogoPixels = logoGrid{
	// terminal cell row 0 (y=0 top, y=1 bot)
	0: {7: 2, 8: 4},
	1: {6: 1, 7: 3, 8: 5, 9: 6},

	// terminal cell row 1 (y=2 top, y=3 bot)
	2: {5: 7, 6: 9, 7: 11, 8: 13, 9: 15, 10: 17},
	3: {5: 8, 6: 10, 7: 12, 8: 14, 9: 16, 10: 18},

	// terminal cell row 2 (y=4 top, y=5 bot)
	4: {4: 19, 5: 21, 6: 23, 7: 25, 8: 26, 9: 27, 10: 29, 11: 31},
	5: {4: 20, 5: 22, 6: 24, 9: 28, 10: 30, 11: 32},

	// terminal cell row 3 (y=6 top, y=7 bot)
	6: {4: 34, 5: 36, 10: 38, 11: 40},
	7: {3: 33, 4: 35, 5: 37, 10: 39, 11: 41, 12: 42},

	// terminal cell row 4 (y=8 top, y=9 bot)
	8: {3: 44, 4: 45, 11: 46, 12: 47},
	9: {2: 43, 3: 45, 12: 48, 13: 49},
}

// pigasusLogoPixels is the canonical Pigasus CLI banner logo: the reference
// logo rotated 180° ("both"), representing the inverted winged delta.
var pigasusLogoPixels = flipBoth(refLogoPixels)

// logoPixels points to pigasusLogoPixels as the primary logo of this CLI.
var logoPixels = pigasusLogoPixels

// flipBoth rotates the 2-D raster 180° (flipHoriz ∘ flipVert).
func flipBoth(g logoGrid) logoGrid {
	var out logoGrid
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			out[y][x] = g[9-y][15-x]
		}
	}
	return out
}

// flipVert reflects the 2-D raster vertically across the horizontal axis:
// sub-pixel row y moves to 9 - y.
//
// Because the underlying unit is the square color block itself, row reflection
// naturally reflects the sub-pixels without needing any cell-internal swap logic.
func flipVert(g logoGrid) logoGrid {
	var out logoGrid
	for y := 0; y < 10; y++ {
		out[y] = g[9-y]
	}
	return out
}

// flipHoriz reflects the 2-D raster horizontally across the vertical axis:
// sub-pixel col x moves to 15 - x.
func flipHoriz(g logoGrid) logoGrid {
	var out logoGrid
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			out[y][x] = g[y][15-x]
		}
	}
	return out
}

// reverseRows swaps terminal rows (cell rows r <-> 4-r) while leaving the
// internal top/bottom sub-pixel orientation untouched.
func reverseRows(g logoGrid) logoGrid {
	var out logoGrid
	for r := 0; r < 5; r++ {
		out[2*r] = g[2*(4-r)]
		out[2*r+1] = g[2*(4-r)+1]
	}
	return out
}

// invertColors swaps top and bottom sub-pixels within each terminal cell.
func invertColors(g logoGrid) logoGrid {
	var out logoGrid
	for r := 0; r < 5; r++ {
		out[2*r] = g[2*r+1]
		out[2*r+1] = g[2*r]
	}
	return out
}

// renderHalfBlock emits the minimal ANSI escape sequence for one terminal
// cell formed by two vertically stacked sub-pixels (top and bot).
func renderHalfBlock(b *strings.Builder, top, bot byte) {
	switch {
	case top == 0 && bot == 0:
		b.WriteByte(' ')
	case top != 0 && bot == 0:
		rgb := logoPalette[top]
		fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm▀\x1b[39m", rgb[0], rgb[1], rgb[2])
	case top == 0 && bot != 0:
		rgb := logoPalette[bot]
		fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm▄\x1b[39m", rgb[0], rgb[1], rgb[2])
	default:
		tRGB := logoPalette[top]
		bRGB := logoPalette[bot]
		fmt.Fprintf(b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀\x1b[39m\x1b[49m",
			tRGB[0], tRGB[1], tRGB[2],
			bRGB[0], bRGB[1], bRGB[2])
	}
}

// renderLogoRow encodes one terminal row r (0..4) of the raster grid as a string.
func renderLogoRow(g logoGrid, r int) string {
	var b strings.Builder
	for c := 0; c < 16; c++ {
		renderHalfBlock(&b, g[2*r][c], g[2*r+1][c])
	}
	return b.String()
}

// renderGridLines renders all 5 terminal rows of the given raster grid.
func renderGridLines(g logoGrid) [5]string {
	var out [5]string
	for r := 0; r < 5; r++ {
		out[r] = renderLogoRow(g, r)
	}
	return out
}

// logoBlockGrid is a 5×6 matrix of RGB color blocks.
// Height is 5 terminal cell rows (r=0..4); width is 6 color blocks (c=0..5).
// Each non-transparent color block is rendered as 2 terminal columns ("  ") with 24-bit background color.
// A cell with {0, 0, 0} is transparent (rendered as 2 plain spaces "  ").
//
// Sub-pixel Architecture Equivalence:
// Fundamentally, this 5×6 color block grid is isomorphic to a 10×12 sub-pixel raster
// where each 2×2 "田字方块" (4 sub-pixels = 2 vertical half-block rows × 2 horizontal columns)
// shares the exact same color. Within each terminal cell, top == bot (so it renders as a full
// block rather than a split half-block), and 2 adjacent terminal cells side-by-side share the color,
// yielding the macroscopic 5×6 visual color block presentation.
type logoBlockGrid [5][6][3]byte

// Subpixels expands the 5×6 color block grid into its underlying 10×12 sub-pixel raster.
// Each 5×6 block maps to a 2×2 "田字方块" of 4 identical sub-pixels.
func (g logoBlockGrid) Subpixels() [10][12][3]byte {
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

// refLogoBlockGrid is the reference Antigravity CLI 5×6 color block logo matrix,
// reverse-engineered from agy 1.4.4 (google3/third_party/jetski/cli/logo/logo.renderAgyLogoBlock).
// Active blocks: exactly 14 non-empty color blocks across the 30 cells.
//
//	Row 0:   .       .     #f2922e #f07236   .       .
//	Row 1:   .     #9ec345 #e2993d #f67a34 #ef5442   .
//	Row 2:   .     #7cc251 #5ca98f #746fc3 #995da8   .
//	Row 3:   .     #61c37d   .       .     #4a80ea   .
//	Row 4: #67b9f4 #64b6f6   .       .     #3883f9 #3d85fc
var refLogoBlockGrid = logoBlockGrid{
	0: {2: {242, 146, 46}, 3: {240, 114, 54}},                                         // #f2922e, #f07236
	1: {1: {158, 195, 69}, 2: {226, 153, 61}, 3: {246, 122, 52}, 4: {239, 84, 66}},   // #9ec345, #e2993d, #f67a34, #ef5442
	2: {1: {124, 194, 81}, 2: {92, 169, 143}, 3: {116, 111, 195}, 4: {153, 93, 168}}, // #7cc251, #5ca98f, #746fc3, #995da8
	3: {1: {97, 195, 125}, 4: {74, 128, 234}},                                         // #61c37d, #4a80ea
	4: {0: {103, 185, 244}, 1: {100, 182, 246}, 4: {56, 131, 249}, 5: {61, 133, 252}}, // #67b9f4, #64b6f6, #3883f9, #3d85fc
}

// flipBothBlocks rotates the 5×6 color block grid 180°:
//
//	r' = 4 - r
//	c' = 5 - c
func flipBothBlocks(g logoBlockGrid) logoBlockGrid {
	var out logoBlockGrid
	for r := 0; r < 5; r++ {
		for c := 0; c < 6; c++ {
			out[r][c] = g[4-r][5-c]
		}
	}
	return out
}

// flipVertBlocks mirrors the 5×6 color block grid vertically across the horizontal axis:
//
//	r' = 4 - r
//	c' = c
func flipVertBlocks(g logoBlockGrid) logoBlockGrid {
	var out logoBlockGrid
	for r := 0; r < 5; r++ {
		out[r] = g[4-r]
	}
	return out
}

// flipHorizBlocks mirrors the 5×6 color block grid horizontally across the vertical axis:
//
//	r' = r
//	c' = 5 - c
func flipHorizBlocks(g logoBlockGrid) logoBlockGrid {
	var out logoBlockGrid
	for r := 0; r < 5; r++ {
		for c := 0; c < 6; c++ {
			out[r][c] = g[r][5-c]
		}
	}
	return out
}

// pigasusLogoBlockGrid is the canonical Wagyu/Pigasus 5×6 color block logo:
// the reference logo rotated 180° ("both"), representing the inverted winged delta.
//
//	Row 0: #3d85fc #3883f9   .       .     #64b6f6 #67b9f4
//	Row 1:   .     #4a80ea   .       .     #61c37d   .
//	Row 2:   .     #995da8 #746fc3 #5ca98f #7cc251   .
//	Row 3:   .     #ef5442 #f67a34 #e2993d #9ec345   .
//	Row 4:   .       .     #f07236 #f2922e   .       .
var pigasusLogoBlockGrid = flipBothBlocks(refLogoBlockGrid)

// logoBlockGridCurrent is the active 5×6 color block grid used by this CLI.
var logoBlockGridCurrent = pigasusLogoBlockGrid

// renderBlockLogoRow renders row r (0..4) of the 5×6 color block grid.
// Each color block occupies 2 terminal character columns ("  ") with 24-bit background color.
// The visual width of the rendered row is exactly 16 terminal columns:
//
//	2 leading spaces + (6 blocks × 2 columns = 12 columns) + 2 trailing spaces = 16 columns.
//
// This visual width of 16 columns perfectly matches renderLogoRow (16 columns),
// so that with the 2-space gutter in logoLines ("%s  ..."), the banner text begins
// at column 19 (18 spaces from the left margin), matching agy 1:1.
func renderBlockLogoRow(g logoBlockGrid, r int) string {
	var b strings.Builder
	// Row 0 begins with ANSI reset \x1b[0m matching reference agy behavior.
	if r == 0 {
		b.WriteString("\x1b[0m")
	}
	b.WriteString("  ") // 2 leading spaces
	for c := 0; c < 6; c++ {
		rgb := g[r][c]
		if rgb == ([3]byte{0, 0, 0}) {
			b.WriteString("  ")
		} else {
			fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm  \x1b[0m", rgb[0], rgb[1], rgb[2])
		}
	}
	b.WriteString("  ") // 2 trailing spaces
	return b.String()
}

// renderBlockGridLines renders all 5 terminal rows of the given 5×6 color block grid.
func renderBlockGridLines(g logoBlockGrid) [5]string {
	var out [5]string
	for r := 0; r < 5; r++ {
		out[r] = renderBlockLogoRow(g, r)
	}
	return out
}

type logoStyle int

const (
	logoStyleDefault logoStyle = iota // 10×16 sub-pixel raster using half-block Unicode characters
	logoStyleSimple                   // 5×6 color block matrix using full-cell background blocks
	logoStyleText                     // text-only (suppressed logo art)
)

// detectLogoStyle determines the logo rendering style based on environment variables
// and terminal auto-detection.
//
// Precedence:
//  1. AGY_CLI_LOGO_STYLE or WAGYU_LOGO_STYLE:
//     - "simple", "block", "blocks", "5x6" -> logoStyleSimple
//     - "default", "half", "halfblock", "half-block" -> logoStyleDefault
//     - "text", "none", "off" -> logoStyleText
//  2. Terminal auto-detection:
//     If running inside iTerm2 (TERM_PROGRAM=iTerm.app or LC_TERMINAL=iTerm2)
//     or Apple Terminal (TERM_PROGRAM=Apple_Terminal), default to logoStyleSimple (5×6 color blocks).
//  3. Default fallback: logoStyleDefault (10×16 half blocks).
func detectLogoStyle() logoStyle {
	val := strings.ToLower(strings.TrimSpace(os.Getenv("AGY_CLI_LOGO_STYLE")))
	if val == "" {
		val = strings.ToLower(strings.TrimSpace(os.Getenv("WAGYU_LOGO_STYLE")))
	}
	switch val {
	case "text", "none", "off":
		return logoStyleText
	case "simple", "block", "blocks", "5x6":
		return logoStyleSimple
	case "default", "half", "halfblock", "half-block":
		return logoStyleDefault
	}

	// Auto-detect based on terminal environment:
	// Apple Terminal, iTerm2, etc. use the simple 5×6 full-cell color block mode.
	tp := os.Getenv("TERM_PROGRAM")
	lt := os.Getenv("LC_TERMINAL")
	if tp == "Apple_Terminal" || tp == "iTerm.app" || lt == "iTerm2" {
		return logoStyleSimple
	}

	return logoStyleDefault
}

// currentLogoGrid returns the active logo raster depending on the active brand.
func currentLogoGrid() logoGrid {
	if branding.Current().FlippedLogo {
		return pigasusLogoPixels
	}
	return refLogoPixels
}

// currentLogoBlockGrid returns the active 5×6 color block grid depending on the active brand.
func currentLogoBlockGrid() logoBlockGrid {
	if branding.Current().FlippedLogo {
		return pigasusLogoBlockGrid
	}
	return refLogoBlockGrid
}

// renderLogoLines renders all 5 terminal rows of the canonical logo.
func renderLogoLines() [5]string {
	switch detectLogoStyle() {
	case logoStyleSimple:
		return renderBlockGridLines(currentLogoBlockGrid())
	case logoStyleText:
		return [5]string{}
	default:
		return renderGridLines(currentLogoGrid())
	}
}
