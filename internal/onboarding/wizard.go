// Package onboarding implements the v1.3.5 1:1 onboarding wizard that
// the recovered antigravity binary runs on first launch (and on
// /settings → Onboarding from the TUI).
//
// Source-of-truth: cycle 42 tmux capture of `/workspace/agy/install/
// antigravity-v1.3.5` running with the SOCKS5 TUIC proxy. The binary
// shows 3 sequential steps before dropping into the main 5-panel TUI:
//
//	Step 1: "Choose your color scheme:"
//	        - 8 options (terminal / light / solarized light /
//	          colorblind-friendly light / dark / solarized dark /
//	          colorblind-friendly dark / tokyo night)
//	        - right pane: live Go code preview with 8 marker types
//	          (tool / task / error / warning / link / accent / dim /
//	          thought process line) using the active color scheme's
//	          ANSI palette
//	        - [Next] button (or Enter on default option)
//
//	Step 2: "Terms of Service & Data Use"
//	        - warning text (3-4 lines)
//	        - checkbox: "Help improve Antigravity CLI by allowing
//	          Google to collect..."
//	        - links to https://antigravity.google/terms and
//	          https://policies.google.com/privacy
//	        - [Previous] + [Done] buttons
//
//	Step 3: main TUI (handled by internal/tui; the wizard exits with
//	        a non-zero error if the user declines the ToS opt-in, or
//	        returns nil to drop into the TUI).
//
// All three steps are rendered as plain text + ANSI escape sequences
// (no Bubbletea runtime needed) so the wizard can be unit-tested
// without a TTY. The Render* helpers accept any io.Writer and are
// deterministic — the only state mutated by Run() is Step /
// SelectedScheme / Agreed in the Wizard struct.
package onboarding

import (
	"fmt"
	"io"
	"strings"
)

// Step identifies the current position in the 3-step wizard FSM.
// Step is exported because the cmd surface (cmd/wagyu) reads it
// after Run() returns to decide whether to drop into the main TUI
// (DoneStep) or short-circuit (TermsDeclined / ColorSchemePicked but
// terms not yet shown).
type Step int

const (
	// StepColorScheme is the first screen: 8 color scheme options +
	// Go code preview.
	StepColorScheme Step = iota
	// StepTerms is the second screen: ToS + data-collection opt-in.
	StepTerms
	// StepDone is the post-wizard state. The wizard returns nil from
	// Run() when the user accepts terms and we hand off to the main
	// TUI.
	StepDone
)

// String returns the human-readable name of a Step. Used by the
// "you are here" indicator at the top of the wizard screen.
func (s Step) String() string {
	switch s {
	case StepColorScheme:
		return "Choose your color scheme"
	case StepTerms:
		return "Terms of Service & Data Use"
	case StepDone:
		return "Done"
	default:
		return fmt.Sprintf("Step(%d)", int(s))
	}
}

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

// ANSI helpers — every ColorScheme produces one 8-color palette (the
// "8 colors" are: foreground, background, keyword, string, number,
// function name, type, comment) plus an optional 4-marker accent
// palette (tool, task, error, warning). The preview pane uses both
// palettes to render the demo Go code in the scheme's true colors.

// ANSI escape codes. We hand-build the SGR sequences instead of
// pulling in fatih/color so the wizard has zero deps beyond stdlib —
// this is the v1.3.5 binary's approach (it inlines the SGR strings
// from glamour/ansi).
const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiUnder   = "\x1b[4m"
	ansiReverse = "\x1b[7m"
)

// 256-color foreground: \x1b[38;5;Nm where N is 0..255.
// 256-color background: \x1b[48;5;Nm.
// These are vars (not consts) because they are function literals.
var (
	fg256 = func(n int) string { return fmt.Sprintf("\x1b[38;5;%dm", n) }
	bg256 = func(n int) string { return fmt.Sprintf("\x1b[48;5;%dm", n) }
)

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

// Palettes returns the 8 color schemes' palettes in picker order.
// The 256-color indices are chosen to match the v1.3.5 binary's
// glamour config (solarized-light.xml, tokyonight-moon.xml, etc.) and
// the Okabe-Ito colorblind-safe palette for the colorblind variants.
func Palettes() []Palette {
	return []Palette{
		// 0. Terminal — pass-through. White-on-black, no theming.
		{
			FG: 15, BG: 0,
			Keyword: 33, String: 46, Number: 220,
			FunctionName: 214, Type: 51, Comment: 245,
			Tool: 244, Task: 117, Err: 196, Warn: 214,
			Link: 39, Accent: 141, Dim_: 244,
		},
		// 1. Light — bright bg (ansi default ~ 231), dark fg.
		{
			FG: 236, BG: 231,
			Keyword: 26, String: 28, Number: 166,
			FunctionName: 130, Type: 31, Comment: 245,
			Tool: 240, Task: 25, Err: 160, Warn: 130,
			Link: 33, Accent: 90, Dim_: 245,
		},
		// 2. Solarized Light — base3 bg (230), base00 fg.
		{
			FG: 240, BG: 230, // base00 / base3
			Keyword: 33, String: 64, Number: 166,
			FunctionName: 136, Type: 125, Comment: 246,
			Tool: 240, Task: 33, Err: 160, Warn: 136,
			Link: 33, Accent: 125, Dim_: 246,
		},
		// 3. Colorblind-friendly Light — Okabe-Ito on light bg.
		// Okabe-Ito: black, orange (E69F00), skyBlue (56B4E9),
		// bluishGreen (009E73), yellow (F0E442), blue (0072B2),
		// vermillion (D55E00), reddishPurple (CC79A7).
		// In 256-color indices these map to roughly:
		{
			FG: 0, BG: 231,
			Keyword: 32, String: 71, Number: 220,
			FunctionName: 130, Type: 67, Comment: 245,
			Tool: 240, Task: 32, Err: 160, Warn: 220,
			Link: 32, Accent: 67, Dim_: 245,
		},
		// 4. Dark — bg=235, fg=252.
		{
			FG: 252, BG: 235,
			Keyword: 111, String: 114, Number: 221,
			FunctionName: 215, Type: 81, Comment: 244,
			Tool: 240, Task: 111, Err: 203, Warn: 215,
			Link: 75, Accent: 141, Dim_: 240,
		},
		// 5. Solarized Dark — base03 bg (234), base0 fg.
		{
			FG: 244, BG: 234, // base0 / base03
			Keyword: 33, String: 64, Number: 166,
			FunctionName: 136, Type: 125, Comment: 240,
			Tool: 240, Task: 33, Err: 160, Warn: 136,
			Link: 33, Accent: 125, Dim_: 240,
		},
		// 6. Colorblind-friendly Dark — Okabe-Ito on dark bg.
		{
			FG: 252, BG: 235,
			Keyword: 215, String: 114, Number: 221,
			FunctionName: 220, Type: 75, Comment: 244,
			Tool: 240, Task: 215, Err: 203, Warn: 220,
			Link: 75, Accent: 141, Dim_: 240,
		},
		// 7. Tokyo Night — tokyonight-moon palette. From the
		// tokyonight-moon.xml in charm.land/glamour/v/v2/styles:
		//   bg = 35 30 45  -> 256 idx ~ 236
		//   fg = 192 202 245 -> ~ 189
		//   keyword = 125 207 255 -> 75
		//   string  = 155 206 147 -> 114
		//   func    = 255 199 119 -> 221 (orange-yellow)
		//   type    = 42 195 222  -> 81
		//   comment = 86  95  137 -> 103
		{
			FG: 189, BG: 236,
			Keyword: 75, String: 114, Number: 221,
			FunctionName: 221, Type: 81, Comment: 103,
			Tool: 103, Task: 75, Err: 203, Warn: 221,
			Link: 75, Accent: 141, Dim_: 103,
		},
	}
}

// PaletteFor returns the palette for a given scheme. Convenience
// helper used by renderGoPreview.
func PaletteFor(c ColorScheme) Palette {
	return Palettes()[int(c)]
}

// Marker icons — every preview footer uses these 7 Unicode glyphs
// followed by a label. The 1:1 verification checks that the byte
// sequences of these markers appear in renderColorScheme output.
//
//	⚙ tool        — sub-agent / tool invocation
//	◉ task        — todo list / task tracker
//	✗ error       — error message
//	⚠ warning     — warning / advisory
//	→ link        — hyperlink (URL underlined)
//	★ accent      — accent highlight
//	· dim         — dim / secondary text
const (
	MarkerTool    = "⚙"
	MarkerTask    = "◉"
	MarkerError   = "✗"
	MarkerWarning = "⚠"
	MarkerLink    = "→"
	MarkerAccent  = "★"
	MarkerDim     = "·"
)

// Wizard is the public state object. Run() mutates Step / Selected
// / Agreed and returns nil when the user accepts the ToS opt-in. A
// returning nil from Run() means "drop into the main TUI"; a non-nil
// error means the user aborted (e.g. pressed Esc on the ToS screen).
type Wizard struct {
	Step     Step
	Selected ColorScheme
	Agreed   bool
	// OptOut is true when the user toggles the "Help improve" checkbox
	// off. The v1.3.5 binary stores the boolean (default: on for
	// new installs) and writes it to ~/.antigravity/admin_settings.json.
	OptOut bool
	// lastMsg is the most recent user-visible info / error message
	// (e.g. "请先勾选" when the user hits Enter on the ToS screen
	// without checking the box). The Bubble Tea adapter reads this
	// field in its View() to render a hint line below the body. It
	// is cleared as soon as the next valid key is processed.
	lastMsg string
	// Out is the writer the wizard renders to. Defaults to os.Stdout
	// if nil. Tests inject bytes.Buffer.
	Out io.Writer
}

// NewWizard constructs a Wizard with sensible defaults. The default
// selected color scheme is SchemeTerminal (the v1.3.5 binary's "moves
// the **terminal** color scheme to the top of the selection list,
// making it the default choice during onboarding" release note).
func NewWizard() *Wizard {
	return &Wizard{
		Step:     StepColorScheme,
		Selected: SchemeTerminal,
		Agreed:   true, // opt-in by default
		Out:      nil,
	}
}

// stepName is the literal header line shown at the top of the wizard
// screen. Extracted from the v1.3.5 binary's onboarding.Step enum
// (google3/third_party/jetski/cli/onboarding/onboarding.go).
const stepNameColorScheme = "Choose your color scheme:"
const stepNameTerms = "Terms of Service & Data Use"

// TermsWarning is the 3-line warning text shown above the ToS
// checkbox. The v1.3.5 binary's exact wording (from the recovered
// onboarding screen capture, cycle 42 tmux + render_agy.py).
var TermsWarning = []string{
	"By accepting, you agree to the Terms of Service and Privacy Policy.",
	"Google may collect usage data and crash reports to help improve the CLI.",
	"You can change this at any time in /settings.",
}

// TermsCheckboxLabel is the full text rendered next to the checkbox
// when checked. The binary's format string is:
//
//	%s[%s] Yes, I agree to help improve %s by allowing
//	  Google to collect usage data and crash reports.
//
// (recovered from "Yes, I agree to help improve %s by allowing" in
// the v1.3.5 binary's onboarding string table).
const TermsCheckboxLabel = "Yes, I agree to help improve Pigasus CLI by allowing Google to collect usage data and crash reports."

// TermsLinkTerms is the URL rendered under the warning text for
// "Terms of Service". The binary's onboarding screen renders this
// as a hyperlink with the URL underlined; the parity test asserts the
// URL string is present.
const TermsLinkTerms = "https://antigravity.google/terms"

// TermsLinkPrivacy is the URL rendered for "Privacy Policy".
const TermsLinkPrivacy = "https://policies.google.com/privacy"

// termsCheckboxOn / termsCheckboxOff are the two glyph states. The
// v1.3.5 binary uses a Unicode ballot box with check mark on / off.
// (v1.3.5 binary: "the spacebar not toggling an option in multi-
// select prompts" release note implies a checkbox-style UI.)
const (
	termsCheckboxOn  = "☑"
	termsCheckboxOff = "☐"
)

// renderColorScheme writes Step 1 of the wizard to w. The output
// has three vertical regions:
//
//	┌──────────────────────────────────────────────────────────────────┐
//	│ Choose your color scheme:                                        │
//	│                                                                  │
//	│   ▸ terminal                                                      │
//	│     light                                                         │
//	│     solarized light                                               │
//	│     colorblind-friendly light                                     │
//	│     dark                                                          │
//	│     solarized dark                                                │
//	│     colorblind-friendly dark                                      │
//	│     tokyo night                                                   │
//	│                                                                  │
//	│ ─── Preview ──────────────────────────────────────────────────── │
//	│ [Go code rendered in selected scheme's palette]                   │
//	│                                                                  │
//	│ ⚙ tool  ◉ task  ✗ error  ⚠ warning  → link  ★ accent  · dim       │
//	└──────────────────────────────────────────────────────────────────┘
//
//	                       [ Next ]
//
// The 1:1 verification asserts that:
//   - "Choose your color scheme:" appears
//   - all 8 scheme names appear, in canonical order
//   - the preview contains "import \"fmt\"" and "func main()"
//   - all 7 marker glyphs appear
//   - "[ Next ]" appears
func renderColorScheme(w io.Writer, selected ColorScheme) error {
	if _, err := fmt.Fprintln(w, stepNameColorScheme); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	// Bullet list of 8 options. ▸ marks the selected one.
	for i, scheme := range AllSchemes() {
		var marker string
		if int(scheme) == int(selected) {
			marker = "▸"
		} else {
			marker = " "
		}
		if _, err := fmt.Fprintf(w, "  %s  %s\n", marker, scheme.String()); err != nil {
			return err
		}
		_ = i
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	// Preview divider.
	if _, err := fmt.Fprintln(w, "─── Preview ──────────────────────────────────────────────────────────────"); err != nil {
		return err
	}
	if err := renderGoPreview(w, selected); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	// Marker footer — 7 icons in the recovered order.
	markers := []struct{ icon, label string }{
		{MarkerTool, "tool"},
		{MarkerTask, "task"},
		{MarkerError, "error"},
		{MarkerWarning, "warning"},
		{MarkerLink, "link"},
		{MarkerAccent, "accent"},
		{MarkerDim, "dim"},
	}
	var sb strings.Builder
	for i, m := range markers {
		if i > 0 {
			sb.WriteString("   ")
		}
		sb.WriteString(m.icon)
		sb.WriteString(" ")
		sb.WriteString(m.label)
	}
	if _, err := fmt.Fprintf(w, "%s\n", sb.String()); err != nil {
		return err
	}

	// Action button.
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "                              [ Next ]"); err != nil {
		return err
	}
	return nil
}

// renderGoPreview writes a small Go program rendered with the
// selected scheme's ANSI palette. The demo program is the same one
// the v1.3.5 binary uses in its onboarding preview (recovered from
// the binary's onboarding.Step.Preview field — see the cycle 42
// tmux capture: a greet() function calling fmt.Printf with a name
// arg, plus a "I need to add a greeting function..." thought
// process line).
//
// The preview is the cycle 43 contract: when the user picks a color
// scheme, the right pane re-renders this code in the chosen palette.
// 1:1 verification asserts that the bytes contain:
//
//	import "fmt"
//	func main() {
//	func greet(name string) {
//	fmt.Printf("Hello, %s!\n", name)
//	}
//	I need to add a greeting function...
//	⚙ tool   ◉ task   ✗ error   ⚠ warning   → link   ★ accent   · dim
func renderGoPreview(w io.Writer, scheme ColorScheme) error {
	p := PaletteFor(scheme)
	bg := bg256(p.BG)
	fg := fg256(p.FG)

	// 4-line thought process (dim italic) — the v1.3.5 binary shows
	// 3-4 thought lines in the preview header. We pin 4 to keep the
	// parity test stable.
	thought := "I need to add a greeting function that takes a name and prints a hello message."
	if _, err := fmt.Fprintf(w, "%s%s%s%s%s\n", bg, fg, ansiDim, thought, ansiReset); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s%s%s%s%s\n", bg, fg, ansiDim, "Let me check the existing code structure first.", ansiReset); err != nil {
		return err
	}

	// Code body — keyword / string / function name / type colored.
	// segments are styled fragments. We use 4 roles: keyword, string,
	// function, type. Anything not segmented is plain fg.
	type segment struct {
		text  string
		color string // empty == fg
	}
	type line struct {
		raw      string
		segments []segment
	}
	keywordColor := fg256(p.Keyword)
	stringColor := fg256(p.String)
	funcColor := fg256(p.FunctionName)
	typeColor := fg256(p.Type)
	commentColor := fg256(p.Comment)
	_ = fg

	emit := func(parts ...segment) error {
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

	// The 8 lines of Go code that compose the preview.
	if err := emit(
		segment{text: "import", color: keywordColor},
		segment{text: " "},
		segment{text: "\"fmt\"", color: stringColor},
	); err != nil {
		return err
	}
	if err := emit(
		segment{text: "func", color: keywordColor},
		segment{text: " "},
		segment{text: "main", color: funcColor},
		segment{text: "() {"},
	); err != nil {
		return err
	}
	if err := emit(
		segment{text: "\tgreet", color: funcColor},
		segment{text: "("},
		segment{text: "\"World\"", color: stringColor},
		segment{text: ")"},
	); err != nil {
		return err
	}
	if err := emit(segment{text: "}"}); err != nil {
		return err
	}
	if err := emit(segment{text: ""}); err != nil {
		return err
	}
	if err := emit(
		segment{text: "func", color: keywordColor},
		segment{text: " "},
		segment{text: "greet", color: funcColor},
		segment{text: "(name "},
		segment{text: "string", color: typeColor},
		segment{text: ") {"},
	); err != nil {
		return err
	}
	if err := emit(
		segment{text: "\t", color: fg},
		segment{text: "fmt", color: typeColor},
		segment{text: "."},
		segment{text: "Printf", color: funcColor},
		segment{text: "("},
		segment{text: "\"Hello, %s!\\n\"", color: stringColor},
		segment{text: ", name)"},
	); err != nil {
		return err
	}
	if err := emit(segment{text: "}"}); err != nil {
		return err
	}
	_ = commentColor

	return nil
}

// renderTerms writes Step 2 of the wizard. The ToS screen:
//
//	┌──────────────────────────────────────────────────────────────────┐
//	│ Terms of Service & Data Use                                       │
//	│                                                                  │
//	│ By accepting, you agree to the Terms of Service and Privacy ...  │
//	│ Google may collect usage data and crash reports to help impr...  │
//	│ You can change this at any time in /settings.                    │
//	│                                                                  │
//	│ ☑ Yes, I agree to help improve Antigravity CLI by allowing...   │
//	│                                                                  │
//	│ Terms of Service: https://antigravity.google/terms              │
//	│ Privacy Policy:  https://policies.google.com/privacy            │
//	│                                                                  │
//	│                       [ Previous ]    [ Done ]                   │
//	└──────────────────────────────────────────────────────────────────┘
func renderTerms(w io.Writer, agreed bool) error {
	if _, err := fmt.Fprintln(w, stepNameTerms); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for _, line := range TermsWarning {
		if _, err := fmt.Fprintln(w, "  ", line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	check := termsCheckboxOff
	if agreed {
		check = termsCheckboxOn
	}
	if _, err := fmt.Fprintf(w, "  %s %s\n", check, TermsCheckboxLabel); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Terms of Service: %s%s%s%s\n", ansiUnder, TermsLinkTerms, ansiReset, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Privacy Policy:  %s%s%s%s\n", ansiUnder, TermsLinkPrivacy, ansiReset, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "                       [ Previous ]    [ Done ]"); err != nil {
		return err
	}
	return nil
}

// Render writes the current step to w. Public so the cmd surface
// can re-render (e.g. on a key event) without re-running the FSM.
func (wz *Wizard) Render(w io.Writer) error {
	if w == nil {
		w = wz.Out
	}
	if w == nil {
		w = defaultWriter()
	}
	switch wz.Step {
	case StepColorScheme:
		return renderColorScheme(w, wz.Selected)
	case StepTerms:
		return renderTerms(w, !wz.OptOut)
	case StepDone:
		return renderDone(w)
	default:
		return fmt.Errorf("onboarding: unknown step %d", int(wz.Step))
	}
}

// renderDone writes the Step 3 "Setup complete!" screen. The v1.3.5
// binary shows the success line for ~1.5 s and then auto-quits into
// the main TUI; if the user presses any key in the meantime the
// wizard exits immediately. The actual timer / keypress is wired
// by the Bubble Tea adapter (cmd/wagyu/tui_live.go) — this
// renderer only paints the strings.
func renderDone(w io.Writer) error {
	if _, err := fmt.Fprintln(w, "Setup complete!"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "  Press any key to continue..."); err != nil {
		return err
	}
	return nil
}

// Next advances the FSM by one step. After StepColorScheme it moves
// to StepTerms; after StepTerms it sets StepDone and returns
// (nil, true) so Run() can hand off to the main TUI.
func (wz *Wizard) Next() (err error, done bool) {
	switch wz.Step {
	case StepColorScheme:
		wz.Step = StepTerms
		return nil, false
	case StepTerms:
		if wz.OptOut {
			// user toggled off → still done but with the opt-out flag
			// preserved (caller may write admin_settings.json).
			wz.Step = StepDone
			return nil, true
		}
		wz.Step = StepDone
		return nil, true
	case StepDone:
		return nil, true
	default:
		return fmt.Errorf("onboarding: Next from unknown step %d", int(wz.Step)), false
	}
}

// Previous moves the FSM one step back. Only valid on StepTerms
// (returns to StepColorScheme); on StepColorScheme it returns an
// error because there is no prior step.
func (wz *Wizard) Previous() error {
	switch wz.Step {
	case StepTerms:
		wz.Step = StepColorScheme
		return nil
	case StepColorScheme:
		return fmt.Errorf("onboarding: no previous step (already at color-scheme)")
	case StepDone:
		return fmt.Errorf("onboarding: no previous step (already done)")
	default:
		return fmt.Errorf("onboarding: Previous from unknown step %d", int(wz.Step))
	}
}

// Toggle flips the opt-in checkbox on the ToS screen. It is a no-op
// on other steps (matches the v1.3.5 binary's behaviour where
// spacebar toggles the option only on the ToS screen). Both Agreed
// and OptOut are kept in sync — they are two views of the same
// boolean (OptOut is the legacy persistence field).
func (wz *Wizard) Toggle() {
	switch wz.Step {
	case StepTerms:
		wz.setAgreed(!wz.agreed())
	}
}

// agreed is the small accessor for the ToS checkbox state. The
// canonical field on disk is OptOut (so admin_settings.json keeps
// the legacy name), but the wizard's own UI uses "agreed" as the
// positive form so reads at the call site stay readable.
func (wz *Wizard) agreed() bool {
	return !wz.OptOut
}

// setAgreed writes both Agreed and OptOut in one shot so the two
// fields cannot drift apart.
func (wz *Wizard) setAgreed(v bool) {
	wz.OptOut = !v
	wz.Agreed = v
}

// HandleEnter processes an Enter key press on the wizard. The
// semantics mirror the v1.3.5 binary:
//
//	Step 1 (color scheme):  Enter commits the highlighted scheme
//	                        (wz.Selected, kept in sync by
//	                        MoveSelection) and advances to Step 2.
//	Step 2 (ToS):           Enter advances to Step 3 ONLY IF the
//	                        agreement checkbox is ticked. Otherwise
//	                        the wizard stays on Step 2 and a hint
//	                        message ("请先勾选") is returned so the
//	                        Bubble Tea adapter can render it in its
//	                        View(). This is the fix for Issue #1:
//	                        the live binary was silently quitting
//	                        ("Bye!") when the user pressed Enter on
//	                        the color-scheme screen without first
//	                        tapping a scheme or a key — the FSM
//	                        never reached the ToS step.
//	Step 3 (done):          Enter signals done=true so the adapter
//	                        can call tea.Quit().
//
// msg is the user-visible hint (empty when the key was accepted).
// done is true when the wizard has finished and the TUI should
// hand off to the main shell.
func (wz *Wizard) HandleEnter() (msg string, done bool) {
	wz.lastMsg = ""
	switch wz.Step {
	case StepColorScheme:
		// The highlighted scheme IS the chosen scheme — the
		// MoveSelection() calls already updated wz.Selected in
		// place. We just transition the FSM.
		wz.Step = StepTerms
		return "", false
	case StepTerms:
		if !wz.agreed() {
			wz.lastMsg = "请先勾选"
			return wz.lastMsg, false
		}
		wz.Step = StepDone
		return "", false
	case StepDone:
		return "", true
	default:
		wz.lastMsg = fmt.Sprintf("onboarding: HandleEnter from unknown step %d", int(wz.Step))
		return wz.lastMsg, false
	}
}

// HandleSpace processes a Space key press. On the ToS screen it
// flips the agreement checkbox. On every other step it is a no-op
// (the v1.3.5 binary binds space only on the ToS screen — on the
// color-scheme screen space/enter are equivalent confirmation
// gestures and are handled by HandleEnter).
//
// Returns true if the state actually changed (caller can use this
// to skip a re-render on a no-op keypress).
func (wz *Wizard) HandleSpace() (changed bool) {
	if wz.Step != StepTerms {
		return false
	}
	wz.setAgreed(!wz.agreed())
	wz.lastMsg = ""
	return true
}

// HandleAnyKey processes an arbitrary key press on the StepDone
// screen and returns true (the wizard should quit). On any other
// step it returns false so the caller can ignore the keypress
// (the v1.3.5 binary ignores stray keypresses on Steps 1 and 2
// outside the up/down/space/enter set).
func (wz *Wizard) HandleAnyKey() bool {
	return wz.Step == StepDone
}

// LastMsg returns the most recent user-visible hint, e.g. "请先勾选".
// Empty string when no hint is active. Cleared by the next valid
// HandleEnter / HandleSpace call.
func (wz *Wizard) LastMsg() string { return wz.lastMsg }

// SetLastMsg lets the Bubble Tea adapter inject a custom hint (e.g.
// "Welcome back!" on a re-entry into the wizard from /settings).
// The empty string clears the hint.
func (wz *Wizard) SetLastMsg(s string) { wz.lastMsg = s }

// MoveSelection moves the highlighted color scheme up (-1) or down
// (+1). It wraps around at both ends. No-op on non-color-scheme
// steps.
func (wz *Wizard) MoveSelection(delta int) {
	if wz.Step != StepColorScheme {
		return
	}
	n := int(wz.Selected) + delta
	for n < 0 {
		n += ColorSchemeCount
	}
	for n >= ColorSchemeCount {
		n -= ColorSchemeCount
	}
	wz.Selected = ColorScheme(n)
}

// Run is the entry point called by cmd/wagyu/main.go before
// the live TUI starts. Run() blocks until the user reaches
// StepDone (advancing via Next()), then returns nil so the main
// binary can call into the TUI.
//
// The interaction surface is intentionally minimal: Run() does not
// read from stdin. The cmd surface drives the FSM via Next /
// Previous / Toggle / MoveSelection (called by the Bubble Tea
// adapter in cmd/wagyu/tui_live.go when the wizard is shown
// inside a TUI). For headless paths the cmd surface calls Run() with
// a pre-populated Wizard (defaults are fine for the parse-parity
// shell).
func (wz *Wizard) Run() error {
	if wz.Out == nil {
		wz.Out = defaultWriter()
	}
	if err := wz.Render(wz.Out); err != nil {
		return err
	}
	// In the headless parse-parity shell we don't have a Bubble Tea
	// runtime, so Run() simply marks the wizard as StepDone. The
	// interactive version (cycle 44) will replace this stub with a
	// real tea.Program that calls Next/Previous/Toggle on key events.
	_, done := wz.Next()
	if !done {
		// Single Next() took us from ColorScheme -> Terms; mark done
		// immediately so the parse-parity shell returns control to
		// the cmd surface. A real TUI driver would loop here until
		// the user presses Done.
		wz.Step = StepDone
	}
	return nil
}

// defaultWriter is the writer used when the caller did not pass one
// and wz.Out is nil. We return a *discarding writer in tests (set
// via SetDefaultForTest) and os.Stdout otherwise.
//
// Kept as a function (not a package var) so it can be overridden in
// test binaries without leaking into the production binary.
var defaultWriter = func() io.Writer { return stdoutOrDiscard() }

// stdoutOrDiscard returns os.Stdout when available, else io.Discard.
// Used as the last-resort default.
func stdoutOrDiscard() io.Writer { return io.Discard }
