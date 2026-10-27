// scheme.go — Color scheme conversions, colors, and preview layout.
package themepreview

import (
	"strings"

	"github.com/justwasm/pigasus/internal/onboarding"
	"github.com/justwasm/pigasus/palette"
)

// ColorsFor returns the (promptColor, placeholderColor) for a given colorScheme.
//
// The prompt wears the scheme's accent and the placeholder its dim
// foreground — the same two slots the picker keys and the tab bar's
// brand wear, so the prompt follows the selected scheme with the rest
// of the interface.
//
// 【一手】 the slot each one reads was read off agy 1.4.8's own scheme
// table; the clone's own table had the dark schemes wearing solarized
// light's accent, which is why the prompt did not move with the
// scheme.
func ColorsFor(scheme string) (promptColor, placeholderColor string) {
	sc, ok := palette.SchemeByName(strings.ToLower(strings.TrimSpace(scheme)))
	if !ok {
		sc = palette.SolarizedLight
	}
	return sc.Accent.Value(), sc.DimFg.Value()
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
