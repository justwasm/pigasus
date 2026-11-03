// panel_render.go — the /hooks panel's rows.
//
// The three frames are the reference's own:
//
//   - the type list ("Hooks", "<n> hook types", the padded rows);
//   - the matcher list ("<type> — Matchers", the add row, the "match
//     all" row for a tool hook, and the empty state);
//   - the form (the add-matcher form for a tool hook, the add-hook form
//     with its three fields for a lifecycle hook).

package hookspanel

import (
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are read from the palette at draw time.
const (
	// panelTitleOpen is the reference's own title span: a default-fg
	// space, then bold.
	panelTitleOpen = "\x1b[39m \x1b[1m"
	// panelTitleClose closes the title's bold span.
	panelTitleClose = "\x1b[0m"

	// panelGray238C is the dim grey's closing span.
	panelGray238C = "\x1b[39m"

	// panelEffMedOpen is the warning row's foreground (the orange the
	// reference uses for the "hooks run shell commands" line).
	panelEffMedOpen = "\x1b[38;5;172m"

	// searchCursorBlock is the one reverse-video cell the reference draws
	// as the input cursor.
	searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

	// panelBoldOpen is the field label's bold span.
	panelBoldOpen = "\x1b[1m"
)

// Lines renders the panel: the type list, the matcher list or the form.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	switch p.stage {
	case StageMatchers:
		return p.matcherLines()
	case StageForm:
		return p.formLines()
	}
	return p.typeLines()
}

// typeLines is the list of the hook types.
func (p *Panel) typeLines() []string {
	lines := []string{
		panelTitleOpen + "Hooks" + panelTitleClose,
		" " + palette.SubtleOpen + "  " + strconv.Itoa(len(types)) + " hook types" + panelGray238C,
		"",
	}
	for i, r := range types {
		name := string(r.event)
		if pad := 14 - len([]rune(name)); pad > 0 {
			name += strings.Repeat(" ", pad)
		}
		if i == p.cursor {
			lines = append(lines, " "+palette.BlueOpen+"> "+name+palette.BlueClose+"  "+
				palette.SubtleOpen+r.description+panelGray238C)
		} else {
			lines = append(lines, "   "+name+"  "+palette.SubtleOpen+r.description+panelGray238C)
		}
	}
	lines = append(lines, "",
		tuilib.PanelDotHint("   ", "↑/↓", "Navigate", "enter", "Select"))
	return lines
}

// matcherLines is the matcher list of one hook type.
func (p *Panel) matcherLines() []string {
	idx := p.idx
	if idx < 0 || idx >= len(types) {
		idx = 0
	}
	name, desc := string(types[idx].event), types[idx].description
	tool := IsTool(idx)
	addLabel := "+ Add new matcher..."
	if !tool {
		addLabel = "+ Add new hook..."
	}
	lines := []string{
		panelTitleOpen + name + " — Matchers" + panelTitleClose,
		" " + palette.SubtleOpen + "  " + desc + panelGray238C,
		"",
	}
	if p.cursor == 0 {
		lines = append(lines, " "+palette.BlueOpen+"> "+addLabel+palette.BlueClose)
	} else {
		lines = append(lines, "   "+addLabel)
	}
	if tool {
		if p.cursor == 1 {
			lines = append(lines, " "+palette.BlueOpen+"> + Match all (no filter)"+palette.BlueClose)
		} else {
			lines = append(lines, "   + Match all (no filter)")
		}
	}
	lines = append(lines, "   "+palette.SubtleOpen+"No hooks configured"+panelGray238C,
		"",
		tuilib.PanelDotHint("   ", "↑/↓", "Navigate", "enter", "Select",
			"e", "Toggle", "backspace", "Delete", "esc", "Back"),
	)
	return lines
}

// formLines is the add-matcher form (a tool hook) or the add-hook form
// (a lifecycle hook).
func (p *Panel) formLines() []string {
	idx := p.idx
	if idx < 0 || idx >= len(types) {
		idx = 0
	}
	name, desc := string(types[idx].event), types[idx].description
	if IsTool(idx) {
		return p.matcherFormLines(name, desc)
	}
	return p.hookFormLines(name, desc)
}

// matcherFormLines is the add-matcher form of a tool hook.
func (p *Panel) matcherFormLines(name, desc string) []string {
	field := " " + palette.BlueOpen + ">  " + palette.BlueClose
	if p.match != "" {
		field = " " + palette.BlueOpen + "> " + palette.BlueClose + p.match
	}
	return []string{
		panelTitleOpen + "Add new matcher for " + name + panelTitleClose,
		"",
		" " + palette.SubtleOpen + "  " + desc + panelGray238C,
		"",
		" " + panelBoldOpen + "  Matcher:" + panelTitleClose,
		field,
		"",
		" " + palette.SubtleOpen + "  Examples:" + panelGray238C,
		" " + palette.SubtleOpen + "  • Write (single tool)" + panelGray238C,
		" " + palette.SubtleOpen + "  • Write|Edit (multiple tools)" + panelGray238C,
		" " + palette.SubtleOpen + "  • Web.* (regex pattern)" + panelGray238C,
		"",
		tuilib.PanelDotHint("   ", "enter", "Confirm", "esc", "Cancel"),
	}
}

// hookFormLines is the add-hook form of a lifecycle hook.
func (p *Panel) hookFormLines(name, desc string) []string {
	nameField := " " + palette.BlueOpen + "> " + searchCursorBlock
	if p.name != "" {
		nameField = " " + palette.BlueOpen + "> " + palette.BlueClose + p.name
	}
	cmdField := "     " + palette.SubtleOpen + " " + panelGray238C
	if p.field == 1 {
		cmdField = "     " + searchCursorBlock
	}
	return []string{
		panelTitleOpen + "Add new hook" + panelTitleClose,
		"",
		" " + palette.SubtleOpen + "  Event: " + name + " — " + desc + panelGray238C,
		"",
		" " + panelBoldOpen + "  Name:" + panelTitleClose,
		nameField,
		"",
		" " + palette.SubtleOpen + "  Command:" + panelGray238C,
		cmdField,
		"",
		" " + palette.SubtleOpen + "  Timeout (seconds):" + panelGray238C,
		"     " + palette.SubtleOpen + "default: 30" + panelGray238C,
		"",
		"   " + panelEffMedOpen + "⚠ Hooks run shell commands with your full permissions." + panelGray238C,
		"",
		tuilib.PanelDotHint("   ", "tab", "Switch field", "enter", "Confirm", "esc", "Cancel"),
	}
}
