// Package modelpicker provides the model picker rows, search filtering,
// effort slider geometry, and row rendering.
package modelpicker

import (
	"strings"

	"github.com/justwasm/pigasus/internal/palette"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// Effort level constants matching the reference's 3-tier effort geometry.
const (
	EffortLow    = 0
	EffortMedium = 1
	EffortHigh   = 2

	PanelGray238  = "\x1b[38;2;88;110;117m" // dim descriptions
	PanelGray238C = "\x1b[39m"

	PanelTitleOpen  = "\x1b[1m\x1b[39m" // panel title: bold + default fg
	PanelTitleClose = "\x1b[0m"

	PanelEffLowOpen  = "\x1b[38;2;133;153;0m" // effort slider, level 0 (green)
	PanelEffMedOpen  = "\x1b[38;5;172m"       // effort slider, level 1 (orange)
	PanelEffHighOpen = "\x1b[38;5;166m"       // effort slider, level 2 (red-orange)
)

// EffortDescriptions are the three blurbs the /effort slider shows.
// Captured by driving REF's /effort panel with ←/→ (/tmp/agycmp/effort_probe.py).
var EffortDescriptions = [3]string{
	"Faster responses, lighter reasoning — great for simpler tasks",
	"Balanced speed and reasoning quality for most tasks",
	"Deepest reasoning for complex problems — slower but strongest",
}

// EffortBarSegments builds the two coloured runs of the /effort slider
// for level: the lit on run and the dim off run.
func EffortBarSegments(level int) (on, off string) {
	if level < EffortLow || level > EffortHigh {
		level = EffortHigh
	}
	marker := func(i int) string {
		switch {
		case i < level:
			return "●"
		case i == level:
			return "◉"
		}
		return "○"
	}
	var lit, dim strings.Builder
	lit.WriteString(strings.Repeat(" ", 7))
	for i := 0; i < 3; i++ {
		if i <= level {
			lit.WriteString(marker(i))
		} else {
			dim.WriteString(marker(i))
		}
		if i < 2 {
			if i < level {
				lit.WriteString(strings.Repeat("━", 14))
			} else {
				dim.WriteString(strings.Repeat("─", 14))
			}
		}
	}
	dim.WriteString(strings.Repeat(" ", 7))
	return lit.String(), dim.String()
}

// EffortSliderBar renders the horizontal slider bar with navigation arrows.
func EffortSliderBar(level int, colours [3]string) string {
	on, off := EffortBarSegments(level)
	leftArrow := PanelGray238 + "◂" + PanelGray238C
	if level > EffortLow {
		leftArrow = palette.BlueOpen + "◂" + palette.BlueClose
	}
	rightArrow := palette.BlueOpen + "▸" + palette.BlueClose
	if level == EffortHigh {
		rightArrow = PanelGray238 + "▸" + PanelGray238C
	}
	return "  " + leftArrow + " " + colours[level] + on +
		PanelGray238 + off + PanelGray238C + " " + rightArrow
}

// EffortSliderLabels renders the three tier labels below the slider bar.
func EffortSliderLabels(level int, colours [3]string) string {
	groups := [3]string{"      low      ", "    medium     ", "     high      "}
	var labels strings.Builder
	labels.WriteString("    ")
	if level > EffortLow {
		labels.WriteString(PanelGray238)
		for i := 0; i < level; i++ {
			labels.WriteString(groups[i])
		}
	}
	labels.WriteString("\x1b[1m")
	labels.WriteString(colours[level])
	labels.WriteString(groups[level])
	labels.WriteString("\x1b[0m")
	if level < EffortHigh {
		labels.WriteString(PanelGray238)
		for i := level + 1; i < 3; i++ {
			labels.WriteString(groups[i])
		}
		labels.WriteString(PanelGray238C)
	}
	return labels.String()
}

// RebuildEffortSlider renders the full /effort slider geometry with title,
// slider bar, tier labels, description, and keyboard hints.
func RebuildEffortSlider(level, curEff int, annotate bool) []string {
	if level < 0 || level > 2 {
		level = EffortHigh
	}
	colours := [3]string{PanelEffLowOpen, PanelEffMedOpen, PanelEffHighOpen}
	slider := EffortSliderBar(level, colours)
	labels := EffortSliderLabels(level, colours)

	desc := "    \x1b[3m" + PanelGray238 + EffortDescriptions[level]
	if annotate && level == curEff {
		desc += "  (current)"
	}
	desc += "\x1b[0m"

	return []string{
		PanelTitleOpen + "Set Effort" + PanelTitleClose,
		"",
		slider,
		labels,
		desc,
		"",
		tuilib.PanelKeyboardHint("←/→", "Effort", "enter", "Select", "esc", "Go Back"),
	}
}
