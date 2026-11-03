// panel_render.go — the /config panel's three faces.
//
// The panel has the settings list, the inline option submenu and the
// colour scheme's preview card. Only one of them is on screen at a
// time, and the panel picks by its own state.
//
// The colours are read from the palette when a row is drawn, so they
// follow the selected scheme: the panel does not cache them at the time
// it opens.

package configpanel

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/themepreview"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own frame. The title is bold on the default foreground.
const (
	panelTitleOpen  = "\x1b[1m\x1b[39m"
	panelTitleClose = "\x1b[0m"
)

// searchCursorBlock is the `Search:` input cursor the reference draws as
// one reverse-video cell (`\x1b[7m\x1b[37m \x1b[0m`).
const searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

// ansiReset ends a styled span.
const ansiReset = "\x1b[0m"

// Lines renders the panel for a terminal of the given size.
//
// The height decides how many rows the list draws; the width is the
// description's wrap budget.
func (p *Panel) Lines(height, width int) []string {
	if p == nil {
		return nil
	}
	rows := p.Rows()
	if p.submenu {
		return p.submenuLines(rows, height, width)
	}
	// The visible-row budget is measured against the *whole* catalogue,
	// not the filtered list: the reference's window is the settings
	// table's window, and the search box only narrows what is painted.
	visible := VisibleRowsFor(height, len(p.Catalogue()))
	lines := []string{
		panelTitleOpen + "Settings" + panelTitleClose,
		"",
		"  Search: " + p.searchContent(),
		"          " + p.searchUnderline(),
		"",
	}
	if len(rows) == 0 {
		return append(lines, "  No matching settings found.", "", "",
			tuilib.PanelKeyboardHint("esc", "Clear Search/Exit"))
	}
	cursor := p.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	p.adjustWindow(cursor, visible)
	for i := p.window; i < p.window+visible && i < len(rows); i++ {
		lines = append(lines, p.renderItemRow(rows[i], p.Value(rows[i]), i == cursor))
	}
	desc := rows[cursor].Description
	if IsTelemetryRow(rows[cursor]) && auth.IsBusinessAccount() {
		desc = auth.TelemetryBusinessDescription()
	}
	lines = append(lines, p.descriptionLines(desc, width)...)
	if visible < len(rows) {
		lines = append(lines,
			palette.DimGrayOpen+fmt.Sprintf("  [%d-%d of %d items]",
				p.window+1, p.window+visible, len(rows))+palette.DimGrayClose)
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, "", tuilib.PanelKeyboardHint("↑/↓", "Navigate", "←/→", "Change",
		"enter", "Select", "esc", "Clear Search/Exit"))
	return lines
}

// submenuLines renders the inline option selection submenu, shown when
// Enter is pressed on a setting.
func (p *Panel) submenuLines(rows []Row, height, width int) []string {
	if p.cursor < 0 || p.cursor >= len(rows) {
		p.submenu = false
		return p.Lines(height, width)
	}
	row := rows[p.cursor]
	currentVal := p.rawDisplayValue(row)

	if p.submenuCursor < 0 {
		p.submenuCursor = 0
	}
	if p.submenuCursor >= len(row.Values) {
		p.submenuCursor = len(row.Values) - 1
	}

	if row.Name == "Color Scheme" {
		return p.colorSchemePreviewLines(row, currentVal)
	}

	visible := VisibleRowsFor(height, len(p.Catalogue()))
	p.clampWindow(len(rows), height)

	lines := []string{
		panelTitleOpen + "Settings" + panelTitleClose,
		"",
		"  Search: " + p.searchContent(),
		"          " + p.searchUnderline(),
		"",
	}
	lines = append(lines, p.submenuRowLines(rows, currentVal, visible)...)

	desc := row.Description
	if IsTelemetryRow(row) && auth.IsBusinessAccount() {
		desc = auth.TelemetryBusinessDescription()
	}
	lines = append(lines, "")
	for _, wrapped := range WrapText(desc, width) {
		lines = append(lines, "  "+palette.SubtleOpen+wrapped+"\x1b[39m")
	}
	lines = append(lines, "", "", tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Save", "esc", "Cancel"))
	return lines
}

// colorSchemePreviewLines renders the Color Scheme submenu with the
// right-side 22-line preview card.
func (p *Panel) colorSchemePreviewLines(row Row, currentVal string) []string {
	displays := make([]string, len(row.Values))
	for i, v := range row.Values {
		displays[i] = v.Display
	}
	lines := themepreview.BuildPreviewLines(displays, p.submenuCursor, currentVal, palette.BlueBoldOpen, ansiReset)
	return append(lines, "", "", "", "", tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Save", "esc", "Cancel"))
}

// submenuRowLines renders the visible rows of the submenu, marking the
// current value and the cursor position.
func (p *Panel) submenuRowLines(rows []Row, currentVal string, visible int) []string {
	lines := make([]string, 0, visible)
	for i := p.window; i < p.window+visible && i < len(rows); i++ {
		r := rows[i]
		if i == p.cursor {
			lines = append(lines, "  "+r.Name)
			for valIdx, val := range r.Values {
				isCurrent := val.Display == currentVal
				suffix := ""
				if isCurrent {
					suffix = " (current)"
				}
				if valIdx == p.submenuCursor {
					lines = append(lines, palette.BlueBoldOpen+"  > "+val.Display+suffix+ansiReset)
				} else {
					lines = append(lines, "    "+val.Display+suffix)
				}
			}
		} else {
			value := p.Value(r)
			pad := NamePad(r.Name, 29)
			lines = append(lines, "  "+r.Name+strings.Repeat(" ", pad)+value)
		}
	}
	return lines
}

// renderItemRow renders one row of the settings list.
//
// The cursor row is blue; a name of sixteen runes or more is painted as
// one span, a shorter one as a blue name span, an uncoloured pad and a
// blue value span (see configpanel.CursorRowIsSingleSpan).
func (p *Panel) renderItemRow(row Row, value string, isCursor bool) string {
	pad := NamePad(row.Name, 29)
	if !isCursor {
		return "  " + row.Name + strings.Repeat(" ", pad) + value
	}
	if CursorRowIsSingleSpan(row.Name) {
		return palette.BlueOpen + "> " + row.Name + strings.Repeat(" ", pad) + value + palette.BlueClose
	}
	return palette.BlueOpen + "> " + row.Name + palette.BlueClose +
		strings.Repeat(" ", pad) +
		palette.BlueOpen + value + palette.BlueClose
}

// descriptionLines renders the selected row's help text, wrapped to the
// terminal's width.
func (p *Panel) descriptionLines(desc string, width int) []string {
	lines := []string{""}
	for _, wrapped := range WrapText(desc, width) {
		lines = append(lines, "  "+palette.SubtleOpen+wrapped+"\x1b[39m")
	}
	return append(lines, "")
}

// searchContent is the search box's content: the typed text in front of
// the reverse-video cursor cell.
func (p *Panel) searchContent() string {
	if p.query == "" {
		return searchCursorBlock
	}
	return p.query + searchCursorBlock
}

// searchUnderline is the 20-cell `─` row the reference draws under the
// search input, in the selected scheme's separator colour.
func (p *Panel) searchUnderline() string {
	return palette.SeparatorOpen + "────────────────────\x1b[39m"
}

// adjustWindow keeps the scroll window aligned with the cursor so the
// highlighted row always stays visible.
func (p *Panel) adjustWindow(cursor, visible int) {
	if p.window < 0 {
		p.window = 0
	}
	if cursor < p.window {
		p.window = cursor
	}
	if cursor >= p.window+visible {
		p.window = cursor - visible + 1
	}
}

// clampWindow is adjustWindow with the visible-row count derived from
// the terminal's height.
func (p *Panel) clampWindow(rowCount, height int) {
	cursor := p.cursor
	if cursor < 0 || cursor >= rowCount {
		cursor = 0
	}
	if p.window < 0 {
		p.window = 0
	}
	visible := VisibleRowsFor(height, len(p.Catalogue()))
	if cursor < p.window {
		p.window = cursor
	}
	if cursor >= p.window+visible {
		p.window = cursor - visible + 1
	}
}
