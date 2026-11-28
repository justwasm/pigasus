// scheme.go — Color scheme conversions, colors, and preview layout.
package themepreview

import (
	"strings"

	"github.com/justwasm/pigasus/internal/ui/views/onboarding"
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

// minLeftWidth is the narrowest the option column may be. The reference
// keeps a gap after even a short name, so a list of short schemes still
// lines its card up at this column.
const minLeftWidth = 33

// leftGap is the blank run the reference leaves between the widest
// option and the preview card's left border.
const leftGap = 4

// leftWidth returns the width of the option column.
//
// 【一手】 agy 1.5.18 sizes this column from the widest row it is about
// to draw, and only the value that is actually current wears the
// ` (current)` suffix. Measured on the reference, the card's border
// lands at:
//
//	column 33 — current `dark`              (widest row: `    colorblind-friendly light`, 29)
//	column 42 — current `colorblind-friendly dark`  (widest row: that name + ` (current)`, 38)
//	column 43 — current `colorblind-friendly light` (widest row: 39)
//
// i.e. widest row + leftGap, floored at minLeftWidth. Sizing it this way
// rather than from a constant is what keeps a long name from running
// over the column and shoving the card sideways.
func leftWidth(displays []string, currentVal string) int {
	widest := 0
	for _, disp := range displays {
		// Every row wears the four-column marker or indent; only the
		// current one wears the suffix.
		row := len([]rune("    " + disp))
		if disp == currentVal {
			row += len(" (current)")
		}
		if row > widest {
			widest = row
		}
	}
	if w := widest + leftGap; w > minLeftWidth {
		return w
	}
	return minLeftWidth
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

	width := leftWidth(displays, currentVal)

	leftLines := make([]string, 22)
	leftLines[0] = "  Color Scheme" + strings.Repeat(" ", width-len("  Color Scheme"))
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
		pad := width - len([]rune(plainOpt))
		if pad < 0 {
			pad = 0
		}
		leftLines[i+1] = optText + strings.Repeat(" ", pad)
	}
	for i := schemeCount + 1; i < 22; i++ {
		leftLines[i] = strings.Repeat(" ", width)
	}

	lines := make([]string, 0, 22)
	for i := 0; i < 22; i++ {
		lines = append(lines, leftLines[i]+cardLines[i])
	}
	return lines
}
