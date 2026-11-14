package herdr

import (
	"os"
	"strconv"
	"strings"

	"github.com/muesli/termenv"
)

// Palette represents a complete Catppuccin color scheme for the Herdr multiplexer.
// Fully aligns with upstream Herdr's catppuccin (Mocha) and catppuccin-latte (Latte) themes.
type Palette struct {
	Name        string
	IsDark      bool
	AccentBg    string // e.g. active tab pill background
	AccentFg    string // contrast text inside active pill
	PanelBg     string // background for sidebar, tab strip, footer
	ActiveRowBg string // highlighted active workspace or agent card
	SelectionBg string // list selection background
	Surface0Bg  string // inactive tab pill background
	Surface1Bg  string
	SurfaceDim  string // subtle divider color
	Overlay0    string // muted metadata / shortcut text
	Overlay1    string
	Text        string // primary text (workspace name, tab label)
	Subtext0    string // secondary text (git branch, agent status)
	Mauve       string // purple accent
	MauveBg     string
	Green       string // idle / clean status dot
	Yellow      string // working / dirty status dot
	Red         string // blocked / error status dot
}

var (
	// ThemeMocha is the dark Catppuccin flavor (upstream Herdr default dark theme).
	ThemeMocha = Palette{
		Name:        "catppuccin",
		IsDark:      true,
		AccentBg:    "\x1b[48;2;137;180;250m", // #89b4fa
		AccentFg:    "\x1b[38;2;17;17;27m",     // #11111b
		PanelBg:     "\x1b[48;2;24;24;37m",    // #181825
		ActiveRowBg: "\x1b[48;2;30;30;46m",    // #1e1e2e
		SelectionBg: "\x1b[48;2;49;50;68m",    // #313244
		Surface0Bg:  "\x1b[48;2;49;50;68m",    // #313244
		Surface1Bg:  "\x1b[48;2;69;71;90m",    // #45475a
		SurfaceDim:  "\x1b[38;2;49;50;68m",    // #313244
		Overlay0:    "\x1b[38;2;108;112;134m", // #6c7086
		Overlay1:    "\x1b[38;2;127;132;156m", // #7f849c
		Text:        "\x1b[38;2;205;214;244m", // #cdd6f4
		Subtext0:    "\x1b[38;2;166;173;200m", // #a6adc8
		Mauve:       "\x1b[38;2;203;166;247m", // #cba6f7
		MauveBg:     "\x1b[48;2;203;166;247m",
		Green:       "\x1b[38;2;166;227;161m", // #a6e3a1
		Yellow:      "\x1b[38;2;249;226;175m", // #f9e2af
		Red:         "\x1b[38;2;243;139;168m", // #f38ba8
	}

	// ThemeLatte is the light Catppuccin flavor (upstream Herdr light theme).
	// Designed specifically for crisp, high-contrast readability on white and light terminals.
	ThemeLatte = Palette{
		Name:        "catppuccin-latte",
		IsDark:      false,
		AccentBg:    "\x1b[48;2;30;102;245m",  // #1e66f5 (Catppuccin Sapphire / Blue)
		AccentFg:    "\x1b[38;2;255;255;255m", // #ffffff (White text on blue pill)
		PanelBg:     "\x1b[48;2;239;241;245m", // #eff1f5 (Base soft grey-white)
		ActiveRowBg: "\x1b[48;2;220;224;232m", // #dce0e8 (Mantle active card highlight)
		SelectionBg: "\x1b[48;2;189;208;245m", // #bdd0f5
		Surface0Bg:  "\x1b[48;2;204;208;218m", // #ccd0da (Inactive tab pill)
		Surface1Bg:  "\x1b[48;2;188;192;204m", // #bcc0cc
		SurfaceDim:  "\x1b[38;2;188;192;204m", // #bcc0cc (Divider line)
		Overlay0:    "\x1b[38;2;124;127;147m", // #7c7f93 (Muted text / headers)
		Overlay1:    "\x1b[38;2;140;143;161m", // #8c8fa1
		Text:        "\x1b[38;2;76;79;105m",   // #4c4f69 (DARK CRISP SLATE TEXT)
		Subtext0:    "\x1b[38;2;108;111;133m", // #6c6f85 (Secondary info)
		Mauve:       "\x1b[38;2;136;57;239m",  // #8839ef
		MauveBg:     "\x1b[48;2;136;57;239m",
		Green:       "\x1b[38;2;64;160;43m",   // #40a02b (Idle green)
		Yellow:      "\x1b[38;2;223;142;29m",  // #df8e1d (Working amber)
		Red:         "\x1b[38;2;210;15;57m",   // #d20f39 (Blocked red)
	}
)

// DetectPalette automatically determines the best color scheme based on environment variables,
// terminal COLORFGBG settings, and terminal luminance detection.
func DetectPalette() Palette {
	// 1. Check explicit HERDR_THEME environment override
	if env := strings.ToLower(strings.TrimSpace(os.Getenv("HERDR_THEME"))); env != "" {
		switch env {
		case "latte", "light", "catppuccin-latte":
			return ThemeLatte
		case "mocha", "dark", "catppuccin", "catppuccin-mocha":
			return ThemeMocha
		}
	}

	// 2. Check generic THEME environment variable
	if env := strings.ToLower(strings.TrimSpace(os.Getenv("THEME"))); env != "" {
		if env == "latte" || env == "light" || strings.Contains(env, "light") {
			return ThemeLatte
		}
		if env == "mocha" || env == "dark" || strings.Contains(env, "dark") {
			return ThemeMocha
		}
	}

	// 3. Inspect COLORFGBG (standard terminal convention where background is the last parameter)
	// Example values: "15;0" (dark background 0) vs "0;15" (light background 15)
	if cfgbg := os.Getenv("COLORFGBG"); cfgbg != "" {
		parts := strings.Split(cfgbg, ";")
		if len(parts) >= 2 {
			bgPart := parts[len(parts)-1]
			if bgPart == "15" || bgPart == "7" || bgPart == "11" {
				return ThemeLatte
			}
			if n, err := strconv.Atoi(bgPart); err == nil && n >= 8 {
				return ThemeLatte
			}
		}
	}

	// 4. Query terminal background luminance via termenv
	if !termenv.HasDarkBackground() {
		return ThemeLatte
	}

	// Default to ThemeMocha (Catppuccin Mocha)
	return ThemeMocha
}
