// Package modelpicker provides the model picker rows, search filtering,
// effort slider geometry, and row rendering.
package modelpicker

import (
	"strings"
	"unicode/utf8"

	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// Effort level constants matching the reference's 3-tier effort geometry.
const (
	EffortLow    = 0
	EffortMedium = 1
	EffortHigh   = 2

	PanelGray238C = "\x1b[39m"

	PanelTitleOpen  = "\x1b[1m\x1b[39m" // panel title: bold + default fg
	PanelTitleClose = "\x1b[0m"

	PanelEffLowOpen  = "\x1b[38;2;133;153;0m" // effort slider, level 0 (green)
	PanelEffMedOpen  = "\x1b[38;5;172m"       // effort slider, level 1 (orange)
	PanelEffHighOpen = "\x1b[38;5;166m"       // effort slider, level 2 (red-orange)
)

// PanelGray238 is the dim grey the slider's arrows and labels are
// painted in — the selected scheme's muted foreground.
var PanelGray238 = palette.SubtleOpen

func init() {
	palette.OnSchemeChange(refreshColours)
}

// refreshColours re-reads the palette after the selected colour scheme
// changed.
func refreshColours() {
	PanelGray238 = palette.SubtleOpen
}

// EffortTiers are the three words the reference's own slider shows.
//
// They are the tiers the reference's catalogue publishes for its
// three-tier models (Gemini 3.8 Flash). A model the user configured may
// declare more — the Web UI's dispatcher accepts five words — so the
// slider's geometry below is written for any number of tiers and these
// are only the reference's own set.
var EffortTiers = []string{"low", "medium", "high"}

// EffortDescriptions are the three blurbs the /effort slider shows.
// Captured by driving REF's /effort panel with ←/→ (/tmp/agycmp/effort_probe.py).
var EffortDescriptions = [3]string{
	"Faster responses, lighter reasoning — great for simpler tasks",
	"Balanced speed and reasoning quality for most tasks",
	"Deepest reasoning for complex problems — slower but strongest",
}

// cloneEffortDescriptions are the blurbs for the tiers the reference
// never names.
//
// The reference's slider has three places, so it says neither "xhigh"
// nor "max"; a model the user configured may declare either. The words
// below are the clone's own — they follow the reference's shape (a speed
// clause and a strength clause, joined by an em dash) so the panel reads
// the same, but the reference has no text to copy.
var cloneEffortDescriptions = map[string]string{
	"xhigh": "Near-maximum reasoning for very hard problems — slower, barely stronger",
	"max":   "Maximum reasoning depth — slowest, strongest results",
}

// EffortDescription returns the blurb a tier shows under the slider.
//
// The three tiers the reference publishes carry the reference's own
// text, byte for byte. The tiers it does not publish carry the clone's
// own text; an unknown tier falls back to the balanced blurb.
func EffortDescription(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "low":
		return EffortDescriptions[EffortLow]
	case "medium":
		return EffortDescriptions[EffortMedium]
	case "high":
		return EffortDescriptions[EffortHigh]
	}
	if d, ok := cloneEffortDescriptions[strings.ToLower(strings.TrimSpace(tier))]; ok {
		return d
	}
	return EffortDescriptions[EffortMedium]
}

// EffortFieldWidth is the width of the slider's field: the run of cells
// between the two arrows.
//
// Measured on the reference's own capture — "  ◂" + 45 cells + "▸" — and
// it does not change with the tier count: a longer bar packs more tiers
// into the same field.
const EffortFieldWidth = 45

// EffortGroupWidth returns the width of one tier's group — the cells
// its marker sits in plus the connector that follows it.
//
// The reference's three tiers divide the field exactly: 45 / 3 = 15,
// and the group is odd so the margins on either side of the field stay
// symmetric (7 cells each). A model with five tiers divides it the same
// way: 45 / 5 = 9, margins of 4.
func EffortGroupWidth(tiers int) int {
	if tiers <= 0 {
		return EffortFieldWidth
	}
	w := EffortFieldWidth / tiers
	if w%2 == 0 {
		w--
	}
	if w < 1 {
		w = 1
	}
	return w
}

// EffortColours returns the colour of each tier position: green at the
// bottom, red-orange at the top, orange in between.
//
// The reference's own three tiers are the two extremes plus the middle,
// so the list this produces is byte-identical to the colours the
// reference paints. A longer bar repeats the middle colour — the
// reference has no longer bar to copy.
func EffortColours(tiers int) []string {
	if tiers <= 0 {
		return nil
	}
	out := make([]string, tiers)
	for i := range out {
		switch {
		case tiers == 1:
			out[i] = PanelEffMedOpen
		case i == 0:
			out[i] = PanelEffLowOpen
		case i == tiers-1:
			out[i] = PanelEffHighOpen
		default:
			out[i] = PanelEffMedOpen
		}
	}
	return out
}

// EffortBarSegmentsN builds the two coloured runs of the /effort slider
// for a bar with the given number of tiers: the lit on run and the dim
// off run.
//
// The lit run reaches the active marker and stops there — the cells
// after it are dim, not coloured (the reference's own /effort capture).
func EffortBarSegmentsN(level, tiers int) (on, off string) {
	if tiers <= 0 {
		tiers = 1
	}
	if level < 0 || level > tiers-1 {
		level = tiers - 1
	}
	w := EffortGroupWidth(tiers)
	pad := (w - 1) / 2
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
	lit.WriteString(strings.Repeat(" ", pad))
	for i := 0; i < tiers; i++ {
		if i <= level {
			lit.WriteString(marker(i))
		} else {
			dim.WriteString(marker(i))
		}
		if i < tiers-1 {
			if i < level {
				lit.WriteString(strings.Repeat("━", w-1))
			} else {
				dim.WriteString(strings.Repeat("─", w-1))
			}
		}
	}
	dim.WriteString(strings.Repeat(" ", pad))
	return lit.String(), dim.String()
}

// EffortBarSegments builds the two coloured runs of the reference's
// three-tier /effort slider.
func EffortBarSegments(level int) (on, off string) {
	return EffortBarSegmentsN(level, len(EffortTiers))
}

// EffortSliderBarN renders the horizontal slider bar with navigation
// arrows for a bar with the given number of tiers.
//
// The left arrow is dim on the lowest tier and the right arrow is dim on
// the highest one; the reference paints exactly that for its three tiers.
func EffortSliderBarN(level, tiers int, colours []string) string {
	if tiers <= 0 {
		tiers = 1
	}
	if level < 0 || level > tiers-1 {
		level = tiers - 1
	}
	on, off := EffortBarSegmentsN(level, tiers)
	leftArrow := PanelGray238 + "◂" + PanelGray238C
	if level > 0 {
		leftArrow = palette.BlueOpen + "◂" + palette.BlueClose
	}
	rightArrow := palette.BlueOpen + "▸" + palette.BlueClose
	if level >= tiers-1 {
		rightArrow = PanelGray238 + "▸" + PanelGray238C
	}
	return "  " + leftArrow + " " + effortColour(colours, level) + on +
		PanelGray238 + off + PanelGray238C + " " + rightArrow
}

// EffortSliderBar renders the reference's three-tier slider bar.
func EffortSliderBar(level int, colours [3]string) string {
	return EffortSliderBarN(level, len(EffortTiers), colours[:])
}

// EffortSliderLabelsN renders the tier labels below a slider bar with
// the given number of tiers.
//
// Each tier's label is centred inside its own group, and the group is
// as wide as the bar's group — so the label sits under its marker. The
// reference's own three labels are reproduced byte for byte: "      low
//
//	", "    medium     " and "     high      ".
func EffortSliderLabelsN(level int, tiers []string, colours []string) string {
	n := len(tiers)
	if n == 0 {
		return ""
	}
	if level < 0 || level > n-1 {
		level = n - 1
	}
	w := EffortGroupWidth(n)
	groups := make([]string, n)
	for i, name := range tiers {
		groups[i] = centreLabel(name, w)
	}
	var labels strings.Builder
	labels.WriteString("    ")
	if level > 0 {
		labels.WriteString(PanelGray238)
		for i := 0; i < level; i++ {
			labels.WriteString(groups[i])
		}
	}
	labels.WriteString("\x1b[1m")
	labels.WriteString(effortColour(colours, level))
	labels.WriteString(groups[level])
	labels.WriteString("\x1b[0m")
	if level < n-1 {
		labels.WriteString(PanelGray238)
		for i := level + 1; i < n; i++ {
			labels.WriteString(groups[i])
		}
		labels.WriteString(PanelGray238C)
	}
	return labels.String()
}

// EffortSliderLabels renders the reference's three tier labels.
func EffortSliderLabels(level int, colours [3]string) string {
	return EffortSliderLabelsN(level, EffortTiers, colours[:])
}

// RebuildEffortSliderN renders the full /effort slider geometry for a
// bar with the given tiers: title, slider bar, tier labels, description
// and keyboard hints.
//
// The description is the blurb of the tier the marker sits on; the
// "(current)" annotation is added when the level equals the session's
// own effort and the caller asks for it.
func RebuildEffortSliderN(level, curEff int, tiers []string, annotate bool) []string {
	names := tiers
	if len(names) == 0 {
		names = EffortTiers
	}
	n := len(names)
	if level < 0 || level > n-1 {
		level = n - 1
	}
	colours := EffortColours(n)
	slider := EffortSliderBarN(level, n, colours)
	labels := EffortSliderLabelsN(level, names, colours)

	desc := "    \x1b[3m" + PanelGray238 + EffortDescription(names[level])
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

// RebuildEffortSlider renders the reference's three-tier slider.
func RebuildEffortSlider(level, curEff int, annotate bool) []string {
	return RebuildEffortSliderN(level, curEff, EffortTiers, annotate)
}

// centreLabel centres a label inside a group of the given width. The
// label keeps its left bias when it cannot be centred exactly.
func centreLabel(label string, width int) string {
	n := utf8.RuneCountInString(label)
	if n > width {
		return label
	}
	left := (width - n) / 2
	return strings.Repeat(" ", left) + label + strings.Repeat(" ", width-left-n)
}

// effortColour returns the colour of a tier position, falling back to
// the middle colour when the list is short.
func effortColour(colours []string, level int) string {
	if level < 0 || level >= len(colours) {
		return PanelEffMedOpen
	}
	return colours[level]
}
