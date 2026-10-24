package onboarding

import (
	"fmt"
	"io"
	"strings"
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
		Tool: 244, Task: 117, Err: 196, Warn: 214,
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
func PaletteFor(c ColorScheme) Palette {
	return Palettes()[int(c)]
}

// stepNameColorScheme is the literal header line shown at the top of the wizard
// screen. Extracted from the v1.3.5 binary's onboarding.Step enum
// (google3/third_party/jetski/cli/onboarding/onboarding.go).
const stepNameColorScheme = "Choose your color scheme:"

func renderSchemeList(w io.Writer, selected ColorScheme) error {
	for _, scheme := range AllSchemes() {
		marker := " "
		if int(scheme) == int(selected) {
			marker = "▸"
		}
		if _, err := fmt.Fprintf(w, "  %s  %s\n", marker, scheme.String()); err != nil {
			return err
		}
	}
	return nil
}

// renderColorScheme writes Step 1 of the wizard to w.
func renderColorScheme(w io.Writer, selected ColorScheme) error {
	if _, err := fmt.Fprintln(w, stepNameColorScheme); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if err := renderSchemeList(w, selected); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "─── Preview ──────────────────────────────────────────────────────────────"); err != nil {
		return err
	}
	if err := renderGoPreview(w, selected); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if err := renderMarkersFooter(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, "                              [ Next ]")
	return err
}

type previewSegment struct {
	text  string
	color string
}

func emitPreviewLine(w io.Writer, bg, fg string, parts ...previewSegment) error {
	var sb strings.Builder
	sb.WriteString(bg)
	sb.WriteString(fg)
	for _, s := range parts {
		if s.color != "" {
			sb.WriteString(s.color)
		}
		sb.WriteString(s.text)
		if s.color != "" {
			sb.WriteString(ansiReset)
			sb.WriteString(bg)
			sb.WriteString(fg)
		}
	}
	sb.WriteString(ansiReset)
	_, err := fmt.Fprintln(w, sb.String())
	return err
}

func renderPreviewThoughts(w io.Writer, bg, fg string) error {
	thought := "I need to add a greeting function that takes a name and prints a hello message."
	if _, err := fmt.Fprintf(w, "%s%s%s%s%s\n", bg, fg, ansiDim, thought, ansiReset); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s%s%s%s%s\n", bg, fg, ansiDim, "Let me check the existing code structure first.", ansiReset)
	return err
}

func renderPreviewMain(w io.Writer, bg, fg, kwColor, strColor, fnColor string) error {
	if err := emitPreviewLine(w, bg, fg,
		previewSegment{text: "import", color: kwColor},
		previewSegment{text: " "},
		previewSegment{text: "\"fmt\"", color: strColor},
	); err != nil {
		return err
	}
	if err := emitPreviewLine(w, bg, fg,
		previewSegment{text: "func", color: kwColor},
		previewSegment{text: " "},
		previewSegment{text: "main", color: fnColor},
		previewSegment{text: "() {"},
	); err != nil {
		return err
	}
	if err := emitPreviewLine(w, bg, fg,
		previewSegment{text: "\tgreet", color: fnColor},
		previewSegment{text: "("},
		previewSegment{text: "\"World\"", color: strColor},
		previewSegment{text: ")"},
	); err != nil {
		return err
	}
	if err := emitPreviewLine(w, bg, fg, previewSegment{text: "}"}); err != nil {
		return err
	}
	return emitPreviewLine(w, bg, fg, previewSegment{text: ""})
}

func renderPreviewGreet(w io.Writer, bg, fg, kwColor, strColor, fnColor, typeColor string) error {
	if err := emitPreviewLine(w, bg, fg,
		previewSegment{text: "func", color: kwColor},
		previewSegment{text: " "},
		previewSegment{text: "greet", color: fnColor},
		previewSegment{text: "(name "},
		previewSegment{text: "string", color: typeColor},
		previewSegment{text: ") {"},
	); err != nil {
		return err
	}
	if err := emitPreviewLine(w, bg, fg,
		previewSegment{text: "\t", color: fg},
		previewSegment{text: "fmt", color: typeColor},
		previewSegment{text: "."},
		previewSegment{text: "Printf", color: fnColor},
		previewSegment{text: "("},
		previewSegment{text: "\"Hello, %s!\\n\"", color: strColor},
		previewSegment{text: ", name)"},
	); err != nil {
		return err
	}
	return emitPreviewLine(w, bg, fg, previewSegment{text: "}"})
}

// renderGoPreview writes a small Go program rendered with the
// selected scheme's ANSI palette.
func renderGoPreview(w io.Writer, scheme ColorScheme) error {
	p := PaletteFor(scheme)
	bg := bg256(p.BG)
	fg := fg256(p.FG)

	if err := renderPreviewThoughts(w, bg, fg); err != nil {
		return err
	}
	kwColor := fg256(p.Keyword)
	strColor := fg256(p.String)
	fnColor := fg256(p.FunctionName)
	typeColor := fg256(p.Type)

	if err := renderPreviewMain(w, bg, fg, kwColor, strColor, fnColor); err != nil {
		return err
	}
	return renderPreviewGreet(w, bg, fg, kwColor, strColor, fnColor, typeColor)
}
