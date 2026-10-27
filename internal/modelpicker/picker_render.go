// Package modelpicker provides the model picker rows, search filtering,
// effort slider geometry, and row rendering.
package modelpicker

import (
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/palette"
)

// RenderModelPickerRow formats a single picker row with cursor, padding,
// styling, and optional (current) or custom model notes.
func RenderModelPickerRow(r Row, i, cursor int, isCurrent bool) string {
	var row string
	ink := ""
	if r.Dim {
		ink = palette.DimGrayOpen
	}
	if i == cursor {
		// The cursor row is padded to column 30 before the reset, so the
		// reference's "> Claude Opus 4.6 (Thinking)  " keeps its trailing
		// spaces (agy 1.4.5, live).
		row = palette.BlueOpen + "> " + r.Display + strings.Repeat(" ", RowPad(r.Display))
		row += palette.BlueClose
		if isCurrent {
			row += PanelGray238 + " (current)" + PanelGray238C
		}
	} else {
		row = "  " + ink + r.Display
		if ink != "" {
			row += palette.DimGrayClose
		}
		if isCurrent {
			row += strings.Repeat(" ", RowPad(r.Display)) +
				PanelGray238 + " (current)" + PanelGray238C
		}
	}
	if r.Note != "" {
		row += strings.Repeat(" ", RowPad(r.Display)+2) +
			"\x1b[38;5;60m" + r.Note + "\x1b[39m"
	}
	return row
}

// ResolveCatalogueID maps a settings.json "model" value (catalogue
// slug or display label) to the canonical catalogue slug. Returns "" for
// a name no catalogue carries.
func ResolveCatalogueID(val string) string {
	val = strings.TrimSpace(val)
	for _, e := range cli.ModelCatalogue() {
		if e.ID == val || e.Label == val {
			return e.ID
		}
	}
	return ""
}

// StripLeadingSpaces drops up to n leading spaces (used to re-indent
// the borrowed /effort slider rows for the /model embed).
func StripLeadingSpaces(s string, n int) string {
	for n > 0 && len(s) > 0 && s[0] == ' ' {
		s = s[1:]
		n--
	}
	return s
}

// BaseSlug returns the catalogue ID prefix for a panel row's
// display label. Cycle 197: the panel row stores the label
// ("Gemini 3.8 Flash (High)") and the catalogue ID
// ("gemini-3.8-flash-high") is what REF persists to settings.json.
// For labels that don't carry an effort suffix (e.g. "Claude Sonnet
// 4.6 (Thinking)"), the base is the catalogue ID with the trailing
// "-thinking" stripped.
func BaseSlug(label string) string {
	for _, m := range cli.ReferenceModels() {
		if m.Label == label {
			id := m.ID
			for _, suf := range []string{"-high", "-medium", "-low", "-thinking"} {
				if strings.HasSuffix(id, suf) {
					return strings.TrimSuffix(id, suf)
				}
			}
			return id
		}
	}
	return label
}

// PanelModelBaseSlug delegates to BaseSlug to return the catalogue ID
// prefix for a panel row's display label.
func PanelModelBaseSlug(label string) string {
	return BaseSlug(label)
}

// CatalogueEntry looks up the catalogue by slug. Used by
// selectModelEffort to derive the display label from a chosen row's
// catalogue ID — a name like "minimax-m3" maps to "MiniMax M3".
//
// The whole catalogue is searched, the configured models included: a row
// of a model that declares reasoning tiers carries its own id
// ("minimax-m3.1-flash-xhigh") and its own label ("MiniMax M3.1 Flash
// (Xhigh)").
func CatalogueEntry(slug string) (cli.ModelEntry, bool) {
	for _, m := range cli.ModelCatalogue() {
		if m.ID == slug {
			return m, true
		}
	}
	return cli.ModelEntry{}, false
}
