// scheme.go — Color scheme conversions, colors, and preview layout.
package themepreview

import (
	"strings"

	"github.com/justwasm/pigasus/internal/onboarding"
)

// ColorsFor returns the (promptColor, placeholderColor) for a given colorScheme.
func ColorsFor(scheme string) (promptColor, placeholderColor string) {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "dark", "solarized dark", "colorblind-friendly dark":
		return "#268BD2", "#839496"
	case "tokyo night":
		return "#82aaff", "#828bb8"
	default:
		return "#268BD2", "#657B83"
	}
}

// SchemeFromString parses a scheme name into an onboarding.ColorScheme.
func SchemeFromString(s string) onboarding.ColorScheme {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "terminal":
		return onboarding.SchemeTerminal
	case "light":
		return onboarding.SchemeLight
	case "solarized light":
		return onboarding.SchemeSolarizedLight
	case "colorblind-friendly light":
		return onboarding.SchemeColorblindLight
	case "dark":
		return onboarding.SchemeDark
	case "solarized dark":
		return onboarding.SchemeSolarizedDark
	case "colorblind-friendly dark":
		return onboarding.SchemeColorblindDark
	case "tokyo night":
		return onboarding.SchemeTokyoNight
	default:
		return onboarding.SchemeSolarizedLight
	}
}

// BuildPreviewLines formats the 22 composite lines of the Color Scheme preview submenu.
func BuildPreviewLines(displays []string, cursor int, currentVal, blueBoldOpen, ansiReset string) []string {
	schemeCount := len(displays)
	if schemeCount > 8 {
		schemeCount = 8
	}

	hoveredScheme := onboarding.ColorScheme(cursor)
	if cursor < 0 || cursor >= schemeCount {
		hoveredScheme = onboarding.SchemeSolarizedLight
	}
	cardLines := Render(hoveredScheme)

	leftLines := make([]string, 22)
	leftLines[0] = "  Color Scheme" + strings.Repeat(" ", 33-len("  Color Scheme"))
	for i := 0; i < schemeCount; i++ {
		disp := displays[i]
		isCurrent := (disp == currentVal)
		suffix := ""
		if isCurrent {
			suffix = " (current)"
		}
		var optText string
		plainOpt := ""
		if i == cursor {
			optText = blueBoldOpen + "  > " + disp + suffix + ansiReset
			plainOpt = "  > " + disp + suffix
		} else {
			optText = "    " + disp + suffix
			plainOpt = "    " + disp + suffix
		}
		pad := 33 - len([]rune(plainOpt))
		if pad < 0 {
			pad = 0
		}
		leftLines[i+1] = optText + strings.Repeat(" ", pad)
	}
	for i := schemeCount + 1; i < 22; i++ {
		leftLines[i] = strings.Repeat(" ", 33)
	}

	lines := make([]string, 0, 22)
	for i := 0; i < 22; i++ {
		lines = append(lines, leftLines[i]+cardLines[i])
	}
	return lines
}
