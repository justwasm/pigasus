// panel_render.go — the /model picker's frame.
//
// The picker has three faces: the title and the search box, the row
// list, and the embedded effort slider. Only the row list and the
// slider are on screen at a time, and the panel picks by its own state.
//
// The colours are read from the palette when a row is drawn, so they
// follow the selected scheme: the panel does not cache them at the time
// it opens.

package modelpicker

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/palette"
)

// searchCursorBlock is the `Search:` input cursor the reference draws as
// one reverse-video cell (`\x1b[7m\x1b[37m \x1b[0m`).
const searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

// searchUnderline is the 20-cell `─` row the reference draws under the
// Search: input, in the selected scheme's separator colour.
//
// The colour is read live rather than cached at start-up: the reference
// paints the underline in the *selected* scheme's separator colour, so
// it follows a scheme change.
func searchUnderline() string {
	return palette.SeparatorOpen + "────────────────────\x1b[39m"
}

// Lines renders the /model picker.
//
// Byte-for-byte layout recovered from
// /tmp/agycmp/panels/ppref.model.raw.txt lines 12–28:
//
//	Switch Model
//	                        ← blank
//	  Search: ▮
//	          ────────────────────
//	                        ← blank
//	> Gemini 3.8 Flash             (current)      ← pad to col 30, gray238
//	  Claude Sonnet 4.6 (Thinking)
//	  Claude Opus 4.6 (Thinking)
//	  GPT-OSS 120B (Medium)
//	  MiniMax M3                    MiniMax M3 via gateway (openagy wrapper)
//	  Gemini 4 Argon                             ← dim gray 241
//	                        ← blank
//	  Effort  ◂        ●━━━...━◉        ▸        ← /effort bar +8 cells
//	                  low          medium          high
//	            Deepest reasoning…                ← +8 cells, no "(current)"
//	                        ← blank
//	Keyboard: ↑/↓ Navigate  ←/→ Effort  enter Select  esc Go Back
func (p *Panel) Lines(height ...int) []string {
	h := 0
	if len(height) > 0 {
		h = height[0]
	}
	lines := p.header()
	rows := p.Rows()
	if len(rows) == 0 {
		return p.empty(lines)
	}
	cursor := p.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	currentSlug := ResolveCatalogueID(p.currentModelLabel())
	if currentSlug == "" {
		currentSlug = strings.TrimSpace(p.currentModelLabel())
	}

	visible := VisibleRowsFor(h, len(rows))
	p.adjustWindow(cursor, visible)

	start := p.window
	end := p.window + visible
	if end > len(rows) {
		end = len(rows)
	}

	for i := start; i < end; i++ {
		r := rows[i]
		isCurrent := (r.Slug != "" && r.CatalogueID(p.effortCursorFor(r)) == currentSlug) ||
			r.Display == currentSlug || r.Slug == currentSlug
		lines = append(lines, RenderModelPickerRow(r, i, cursor, isCurrent))
	}

	if visible < len(rows) {
		lines = append(lines,
			palette.DimGrayOpen+fmt.Sprintf("  [%d-%d of %d items]",
				start+1, end, len(rows))+palette.DimGrayClose)
	}

	return p.footer(lines, h)
}

// header renders the picker's title and search box.
//
// The search box shows the typed text in front of the cursor block,
// exactly like the /config panel's (agy 1.4.5, live: "  Search:
// claude" + the reverse-video cell).
func (p *Panel) header() []string {
	searchContent := searchCursorBlock
	if p.query != "" {
		searchContent = p.query + searchCursorBlock
	}
	return []string{
		PanelTitleOpen + "Switch Model" + PanelTitleClose,
		"",
		"  Search: " + searchContent,
		"          " + searchUnderline(),
		"",
	}
}

// empty renders the picker's no-match state.
func (p *Panel) empty(lines []string) []string {
	// The reference's empty-result line, byte for byte (agy 1.4.5,
	// live): the foreground is reset before the two spaces, the
	// message itself is 256-colour 60.
	return append(lines,
		"\x1b[39m  \x1b[38;5;60mNo models match \""+p.query+"\"\x1b[39m",
		"",
		tuilib.PanelKeyboardHint("esc", "Clear search"))
}

// footer renders the picker's legend and, when the cursor is on a row
// that has more than one effort, the embedded slider.
func (p *Panel) footer(lines []string, height int) []string {
	// While a search is active the reference's last legend entry reads
	// "esc Clear search" instead of "esc Go Back" (agy 1.4.5, live) —
	// Esc then clears the query and keeps the panel open.
	back := "Go Back"
	if p.query != "" {
		back = "Clear search"
	}
	// At short terminal heights (< 24), the reference hides the effort slider
	// to ensure the panel fits within available viewport.
	if height > 0 && height < 24 {
		return append(lines, "",
			tuilib.PanelKeyboardHint("↑/↓", "Navigate",
				"enter", "Select", "esc", back))
	}
	// Cycle 208: the slider (and its legend entry) only exist while the
	// cursor is on a row that offers more than one effort. The
	// reference's own picker has exactly one such row (the three-tier
	// Gemini); a configured model that declares tiers gets the same
	// slider, widened to fit its own tier count.
	row, ok := p.currentRow()
	if !ok || !row.HasEffortSlider() {
		return append(lines, "",
			tuilib.PanelKeyboardHint("↑/↓", "Navigate",
				"enter", "Select", "esc", back))
	}
	level := p.effortCursor
	if level < 0 || level > len(row.Tiers)-1 {
		level = p.effortCursorFor(row)
	}
	eff := effortpanel.RebuildEffortSliderN(level, effortpanel.EffortLow, row.Tiers, false)
	return append(lines, "",
		"  Effort  "+eff[2][2:],
		"        "+eff[3],
		"            "+StripLeadingSpaces(eff[4], 4),
		"",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "←/→", "Effort",
			"enter", "Select", "esc", back))
}
