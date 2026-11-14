package onboarding

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// ColorScheme is one of the 8 named options in the color-scheme
// picker. The order matches the v1.3.5 binary's "moves the
// **terminal** color scheme to the top" release note — terminal is
// first, light variants before dark, and tokyo night is last.
//
//	0 Terminal                       — pass-through to host terminal
//	1 Light                          — bright bg, dark fg
//	2 Solarized Light                — Ethan Schoonover's solarized
//	3 Colorblind-friendly Light      — Okabe-Ito on light bg
//	4 Dark                           — dark bg, light fg
//	5 Solarized Dark                 — solarized on dark bg
//	6 Colorblind-friendly Dark       — Okabe-Ito on dark bg
//	7 Tokyo Night                    — tokyonight-moon palette
type ColorScheme int

const (
	SchemeTerminal ColorScheme = iota
	SchemeLight
	SchemeSolarizedLight
	SchemeColorblindLight
	SchemeDark
	SchemeSolarizedDark
	SchemeColorblindDark
	SchemeTokyoNight
)

// ColorSchemeCount is the number of selectable color schemes in the
// wizard. The parity test asserts that exactly 8 options are rendered
// in renderColorScheme.
const ColorSchemeCount = 8

// String returns the human-readable label rendered in the picker
// list. The v1.3.5 binary uses these exact strings (extracted from
// the binary's glamour styles directory: solarized-light.xml,
// tokyonight-moon.xml, kanagawa-dragon.xml, modus-operandi.xml,
// aura-theme-dark.xml, rose-pine-dawn.xml, plus a "terminal" pass-
// through).
func (c ColorScheme) String() string {
	switch c {
	case SchemeTerminal:
		return "terminal"
	case SchemeLight:
		return "light"
	case SchemeSolarizedLight:
		return "solarized light"
	case SchemeColorblindLight:
		return "colorblind-friendly light"
	case SchemeDark:
		return "dark"
	case SchemeSolarizedDark:
		return "solarized dark"
	case SchemeColorblindDark:
		return "colorblind-friendly dark"
	case SchemeTokyoNight:
		return "tokyo night"
	default:
		return fmt.Sprintf("ColorScheme(%d)", int(c))
	}
}

// AllSchemes returns the 8 picker options in the v1.3.5 binary's
// display order. The wizard iterates this slice in renderColorScheme
// to draw the bullet list.
func AllSchemes() []ColorScheme {
	return []ColorScheme{
		SchemeTerminal,
		SchemeLight,
		SchemeSolarizedLight,
		SchemeColorblindLight,
		SchemeDark,
		SchemeSolarizedDark,
		SchemeColorblindDark,
		SchemeTokyoNight,
	}
}

// Palette is an 8-color palette plus 4 marker accents. The 8 main
// colors map to the preview pane's syntax-highlighting roles:
//
//	[0] Foreground    — default text
//	[1] Background    — line behind code (used as a dim wash)
//	[2] Keyword       — `import` `func` `return` `package` (blue-ish)
//	[3] String        — `"Hello, %s!\n"` (green-ish)
//	[4] Number        — (rare in this preview)
//	[5] FunctionName  — `greet` `Printf` (yellow)
//	[6] Type          — `string` (cyan)
//	[7] Comment       — `// ...` (gray)
//
// The 4 marker accents are foreground colors for the 7 marker icons
// in the preview footer: [tool] [task] [error] [warning] [link]
// [accent] [dim].
//
// Terminal scheme uses index 7 (white) for foreground and index 0
// (black) for background, falling through to whatever the host
// terminal happens to be.
type Palette struct {
	FG, BG             int
	Keyword            int
	String             int
	Number             int
	FunctionName       int
	Type               int
	Comment            int
	Tool, Task         int
	Err, Warn          int
	Link, Accent, Dim_ int
}

func terminalPalette() Palette {
	return Palette{
		FG: 15, BG: 0,
		Keyword: 33, String: 46, Number: 220,
		FunctionName: 214, Type: 51, Comment: 245,
		Tool: 220, Task: 34, Err: 196, Warn: 214,
		Link: 39, Accent: 141, Dim_: 244,
	}
}

func lightPalettes() []Palette {
	return []Palette{
		terminalPalette(),
		{ // 1. Light — bright bg (ansi default ~ 231), dark fg.
			FG: 236, BG: 231,
			Keyword: 26, String: 28, Number: 166,
			FunctionName: 130, Type: 31, Comment: 245,
			Tool: 240, Task: 25, Err: 160, Warn: 130,
			Link: 33, Accent: 90, Dim_: 245,
		},
		{ // 2. Solarized Light — base3 bg (230), base00 fg.
			FG: 240, BG: 230,
			Keyword: 33, String: 64, Number: 166,
			FunctionName: 136, Type: 125, Comment: 246,
			Tool: 240, Task: 33, Err: 160, Warn: 136,
			Link: 33, Accent: 125, Dim_: 246,
		},
		{ // 3. Colorblind-friendly Light — Okabe-Ito on light bg.
			FG: 0, BG: 231,
			Keyword: 32, String: 71, Number: 220,
			FunctionName: 130, Type: 67, Comment: 245,
			Tool: 240, Task: 32, Err: 160, Warn: 220,
			Link: 32, Accent: 67, Dim_: 245,
		},
	}
}

func darkPalettes() []Palette {
	return []Palette{
		{ // 4. Dark — bg=235, fg=252.
			FG: 252, BG: 235,
			Keyword: 111, String: 114, Number: 221,
			FunctionName: 215, Type: 81, Comment: 244,
			Tool: 240, Task: 111, Err: 203, Warn: 215,
			Link: 75, Accent: 141, Dim_: 240,
		},
		{ // 5. Solarized Dark — base03 bg (234), base0 fg.
			FG: 244, BG: 234,
			Keyword: 33, String: 64, Number: 166,
			FunctionName: 136, Type: 125, Comment: 240,
			Tool: 240, Task: 33, Err: 160, Warn: 136,
			Link: 33, Accent: 125, Dim_: 240,
		},
		{ // 6. Colorblind-friendly Dark — Okabe-Ito on dark bg.
			FG: 252, BG: 235,
			Keyword: 215, String: 114, Number: 221,
			FunctionName: 220, Type: 75, Comment: 244,
			Tool: 240, Task: 215, Err: 203, Warn: 220,
			Link: 75, Accent: 141, Dim_: 240,
		},
		{ // 7. Tokyo Night — tokyonight-moon palette.
			FG: 189, BG: 236,
			Keyword: 75, String: 114, Number: 221,
			FunctionName: 221, Type: 81, Comment: 103,
			Tool: 103, Task: 75, Err: 203, Warn: 221,
			Link: 75, Accent: 141, Dim_: 103,
		},
	}
}

// Palettes returns the 8 color schemes' palettes in picker order.
// The 256-color indices are chosen to match the v1.3.5 binary's
// glamour config (solarized-light.xml, tokyonight-moon.xml, etc.) and
// the Okabe-Ito colorblind-safe palette for the colorblind variants.
func Palettes() []Palette {
	return append(lightPalettes(), darkPalettes()...)
}

// PaletteFor returns the palette for a given scheme. Convenience
// helper used by renderGoPreview.
//
// The four syntax roles are the reference's own: it paints the preview
// card's `import`/`func` in the keyword colour, `"fmt"` in the string
// colour, `main`/`greet` in the function colour and `string` in the
// type colour, and every one of them changes with the scheme.
func PaletteFor(c ColorScheme) Palette {
	p := Palettes()[int(c)]
	syn := palette.SyntaxFor(c.String())
	p.Keyword = syn.Keyword
	p.String = syn.String
	p.FunctionName = syn.Function
	p.Type = syn.Type
	return p
}

// stepNameColorScheme is the literal header line shown at the top of the wizard
// screen. Extracted from the v1.3.5 binary's onboarding.Step enum
// (google3/third_party/jetski/cli/onboarding/onboarding.go).
const stepNameColorScheme = "Choose your color scheme:"

// formatInnerRow truncates or pads a styled string to fit inside innerWidth visible cells.
func formatInnerRow(raw string, innerWidth int) string {
	w := ansi.StringWidth(raw)
	if w > innerWidth {
		raw = ansi.Truncate(raw, innerWidth, "")
		raw += ansiReset
	} else if w < innerWidth {
		raw += strings.Repeat(" ", innerWidth-w)
	}
	return raw
}

// buildPreviewRows constructs the 20 internal lines of the official agy code & markers preview card.
func buildPreviewRows(selected ColorScheme) []string {
	p := PaletteFor(selected)
	kwColor := fg256(p.Keyword)
	strColor := fg256(p.String)
	fnColor := fg256(p.FunctionName)
	typeColor := fg256(p.Type)

	var rows []string

	// 1. User Prompt
	rows = append(rows, " "+ansiBold+"\x1b[34m> you: \x1b[0madd a greeting function")
	// 2. Blank
	rows = append(rows, "")
	// 3. Diff introduction
	rows = append(rows, "   "+ansiBold+"\x1b[32mHere's the change:\x1b[0m")
	// 4. Blank
	rows = append(rows, "")
	// 5. Line 3: import "fmt"
	rows = append(rows, "   \x1b[90m3\x1b[0m     "+ansiBold+kwColor+"import"+ansiReset+"  "+strColor+"\"fmt\""+ansiReset)
	// 6. Line 4: blank
	rows = append(rows, "   \x1b[90m4\x1b[0m")
	// 7. Line 5: deletion - func main() {
	delBg := "\x1b[48;5;131m"
	rows = append(rows, "   \x1b[90m5\x1b[0m"+delBg+"  \x1b[31m-   "+ansiBold+kwColor+"func"+ansiReset+delBg+"  "+fnColor+"main"+ansiReset+delBg+"() {                                       \x1b[49m")
	// 8. Line 5: addition + func greet(name string) {
	addBg := "\x1b[48;5;65m"
	rows = append(rows, "   \x1b[90m5\x1b[0m"+addBg+"  \x1b[32m+   "+ansiBold+kwColor+"func"+ansiReset+addBg+"  "+fnColor+"greet"+ansiReset+addBg+"(name  "+typeColor+"string"+ansiReset+addBg+") {                           \x1b[49m")
	// 9. Line 6: addition + fmt.Printf("Hello, %s!\n", name)
	rows = append(rows, "   \x1b[90m6\x1b[0m"+addBg+"  \x1b[32m+   \x1b[39m    fmt."+fnColor+"Printf"+ansiReset+addBg+"("+strColor+"\"Hello, %s!\\n\""+ansiReset+addBg+", name)                \x1b[49m")
	// 10. Line 7: }
	rows = append(rows, "   \x1b[90m7\x1b[0m    }")
	// 11. Blank
	rows = append(rows, "")
	// 12. Thought Process Header
	rows = append(rows, "  \x1b[37m  ▾ Thought Process\x1b[39m")
	// 13. Thought Process Detail
	rows = append(rows, "  \x1b[37m    I need to add a greeting function. I'll use fmt.Printf.\x1b[39m")
	// 14. Tool execution
	rows = append(rows, "  \x1b[33m  "+MarkerTool+" tool:  \x1b[39mwrite_file main.go")
	// 15. Task item
	rows = append(rows, "  \x1b[35m  "+MarkerTask+" task:  \x1b[39mImplementing greeting")
	// 16. Error indication
	rows = append(rows, "  \x1b[31m  "+MarkerError+" error:  \x1b[39mcompilation failed")
	// 17. Warning indication
	rows = append(rows, "  \x1b[33m  "+MarkerWarning+" warning:  \x1b[39mdeprecation warning")
	// 18. Link item
	rows = append(rows, "   "+MarkerLink+" link:  \x1b[4m\x1b[34mfile:///path/to/main.go\x1b[0m")
	// 19. Accent item
	rows = append(rows, "  "+ansiBold+"\x1b[94m  "+MarkerAccent+" accent:  \x1b[0mhighlighted text")
	// 20. Dim footer hint
	rows = append(rows, "  \x1b[90m  "+MarkerDim+" dim: press Enter to continue\x1b[39m")

	return rows
}

func renderColorSchemeLines(selected ColorScheme, width int) []string {
	var lines []string

	// 1. Google Gemini Sparkle Logo (5 lines)
	logo := tuilib.RenderLogoLines()
	lines = append(lines, "")
	for _, l := range logo {
		lines = append(lines, l)
	}
	lines = append(lines, "")

	// 2. Welcome Banner
	lines = append(lines, "Welcome to \x1b[38;2;66;133;244mAntigravity CLI\x1b[39m!")
	lines = append(lines, "")

	// 3. Narrow layout (< 70 cols): Hide preview card, show (widen the terminal to see a preview)
	if width > 0 && width < 70 {
		lines = append(lines, ansiBold+stepNameColorScheme+ansiReset, "")
		for _, scheme := range AllSchemes() {
			marker := " "
			if scheme == selected {
				marker = ">"
			}
			lines = append(lines, fmt.Sprintf("  %s %s", marker, scheme.String()))
		}
		lines = append(lines, "  \x1b[90m(widen the terminal to see a preview)\x1b[0m")
		lines = append(lines, "")
		lines = append(lines, "    \x1b[1m\x1b[32m[Next]\x1b[0m")
		lines = append(lines, "")
		lines = append(lines, "  \x1b[1m\x1b[94m↑/↓\x1b[0m\x1b[90m Navigate · \x1b[1m\x1b[94menter\x1b[0m\x1b[90m Confirm\x1b[0m")
		if width >= 30 {
			tag := "\x1b[2mGemini 3.8 Flash · high\x1b[0m"
			pad := width - 23
			if pad > 0 {
				lines = append(lines, strings.Repeat(" ", pad)+tag)
			}
		}
		return lines
	}

	// 4. Side-by-Side Responsive Layout (width == 0 || width >= 70)
	leftWidth := 33
	boxWidth := 63
	if width > 0 && width < leftWidth+1+boxWidth {
		boxWidth = width - leftWidth - 1
		if boxWidth < 30 {
			boxWidth = 30
		}
	}
	innerWidth := boxWidth - 2

	// Left Column Lines
	leftCol := make([]string, 0, 22)
	leftCol = append(leftCol, ansiBold+stepNameColorScheme+ansiReset) // row 0: Choose your color scheme:
	leftCol = append(leftCol, "")                                    // row 1: blank
	for _, scheme := range AllSchemes() {                             // rows 2..9: 8 schemes
		marker := " "
		if scheme == selected {
			marker = ">"
		}
		leftCol = append(leftCol, fmt.Sprintf("  %s %s", marker, scheme.String()))
	}
	for len(leftCol) < 22 {
		leftCol = append(leftCol, "")
	}

	// Right Column Card Lines (22 lines total: top border + 20 rows + bottom border)
	cardLines := make([]string, 0, 22)
	cardLines = append(cardLines, "\x1b[90m╭"+strings.Repeat("─", innerWidth)+"╮\x1b[0m")
	previewRows := buildPreviewRows(selected)
	for _, row := range previewRows {
		cardLines = append(cardLines, "\x1b[90m│\x1b[0m"+formatInnerRow(row, innerWidth)+"\x1b[90m│\x1b[0m")
	}
	cardLines = append(cardLines, "\x1b[90m╰"+strings.Repeat("─", innerWidth)+"╯\x1b[0m")

	// Merge Left Column and Right Card side-by-side
	for r := 0; r < 22; r++ {
		l := leftCol[r]
		w := ansi.StringWidth(l)
		pad := leftWidth - w
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, l+strings.Repeat(" ", pad)+" "+cardLines[r])
	}

	lines = append(lines, "")
	lines = append(lines, "    \x1b[1m\x1b[32m[Next]\x1b[0m")
	lines = append(lines, "")
	lines = append(lines, "  \x1b[1m\x1b[94m↑/↓\x1b[0m\x1b[90m Navigate · \x1b[1m\x1b[94menter\x1b[0m\x1b[90m Confirm\x1b[0m")

	targetW := width
	if targetW <= 0 {
		targetW = 97
	}
	tag := "\x1b[2mGemini 3.8 Flash · high\x1b[0m"
	pad := targetW - 23
	if pad > 0 {
		lines = append(lines, strings.Repeat(" ", pad)+tag)
	}

	return lines
}

func renderColorSchemeWithDims(w io.Writer, selected ColorScheme, width, height, scroll int) error {
	lines := renderColorSchemeLines(selected, width)
	if height > 0 && len(lines) > height {
		area := max(1, height-1)
		lines = tuiviewport.ScrollWindow(len(lines), scroll, area).SliceRows(lines)
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	return nil
}

// renderGoPreview writes a small Go program rendered with the
// selected scheme's ANSI palette (kept for backward-compatibility with cycle tests).
func renderGoPreview(w io.Writer, scheme ColorScheme) error {
	rows := buildPreviewRows(scheme)
	for _, r := range rows {
		if _, err := fmt.Fprintln(w, r); err != nil {
			return err
		}
	}
	return nil
}
