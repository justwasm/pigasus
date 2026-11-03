// panel_render.go — the /permissions panel's rows.
//
// The three frames are the reference's own:
//
//   - the scope picker ("Permission Config Editor", the three scopes and
//     the blurb of the scope under the cursor);
//   - the tab view ("Permissions — <scope>", the allowlist/denylist/
//     asklist strip with its counts, the rules of the selected list);
//   - the add/edit form ("Add Rule — <scope> — <list>", the input row
//     with its reverse-video cursor, the format hint and the legend).

package permissionspanel

import (
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are read from the palette at draw time.
const (
	panelTitleOpen  = "\x1b[1m\x1b[39m" // panel title: bold + default fg
	panelTitleClose = "\x1b[0m"

	panelGray238C = "\x1b[39m" // the dim grey's closing span

	panelEffHighOpen = "\x1b[38;5;166m" // the error row's foreground

	// searchCursorBlock is the one reverse-video cell the reference draws
	// as the input cursor.
	searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"
)

// Lines renders the panel: the scope picker, the tab view or the form.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	switch p.stage {
	case StageLists:
		return p.listLines()
	case StageForm:
		return p.formLines()
	}
	return p.scopeLines()
}

// scopeLines is the scope picker.
func (p *Panel) scopeLines() []string {
	lines := []string{
		panelTitleOpen + "Permission Config Editor" + panelTitleClose,
		"",
		palette.SubtleOpen + "Select a config scope to edit:" + panelGray238C,
		"",
	}
	for i, s := range scopes {
		if i == p.cursor {
			lines = append(lines, palette.BlueOpen+"> "+s[0]+palette.BlueClose)
		} else {
			lines = append(lines, "  "+s[0])
		}
	}
	cursor := p.cursor
	if cursor < 0 || cursor >= len(scopes) {
		cursor = 0
	}
	lines = append(lines,
		"",
		"  "+palette.SubtleOpen+scopes[cursor][1]+panelGray238C,
		"",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Save", "esc", "Close"),
	)
	return lines
}

// listLines is the tab view: the selected scope's three lists.
func (p *Panel) listLines() []string {
	scope := p.scope
	if scope < 0 || scope > 2 {
		scope = 0
	}
	var counts [3]int
	for i := 0; i < 3; i++ {
		counts[i] = len(p.readList(scope, i))
	}
	lines := []string{
		panelTitleOpen + "Permissions — " + permissions.PermScopeNames[scope] + panelTitleClose,
		permissions.PermTabRow(p.tab, counts),
		"",
	}
	list := p.readList(scope, p.tab)
	if len(list) == 0 {
		lines = append(lines, palette.SubtleOpen+
			"No "+permissions.PermListNames[p.tab]+" rules."+panelGray238C)
	} else {
		for i, rule := range list {
			if i == p.cursor {
				lines = append(lines, palette.BlueOpen+"> "+rule+palette.BlueClose)
			} else {
				lines = append(lines, "  "+rule)
			}
		}
	}
	lines = append(lines, "",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "←/→", "Switch View",
			"a", "Add rule", "e", "Edit rule", "d/⌫", "Delete rule",
			"esc", "Close"),
	)
	return lines
}

// formLines is the add/edit rule form.
func (p *Panel) formLines() []string {
	scope := p.scope
	if scope < 0 || scope > 2 {
		scope = 0
	}
	verb := "Add"
	if p.form == FormEdit {
		verb = "Edit"
	}
	lines := []string{
		panelTitleOpen + verb + " Rule — " + permissions.PermScopeNames[scope] + " — " +
			permissions.PermListNames[p.tab] + panelTitleClose,
		"",
		palette.SubtleOpen + "Enter a permission rule:" + panelGray238C,
		"",
		p.text + searchCursorBlock,
		"",
	}
	if p.errMsg != "" {
		lines = append(lines, panelEffHighOpen+"✗ "+p.errMsg+panelGray238C, "")
	}
	lines = append(lines,
		palette.SubtleOpen+"Format: action(target)"+panelGray238C,
		"",
		// The reference separates the first two legend pairs with a
		// literal TAB (probe_perm_add.py, 3/3 runs). The clone's cell
		// renderer drops raw control characters, so a literal "\t" never
		// reaches the pane; two spaces preserve the separator instead
		// (see 存疑 in the cycle report).
		"\x1b[1mKeyboard: \x1b[0m"+palette.BlueOpen+"↑/↓"+palette.BlueClose+" "+
			palette.DimGrayOpen+"Navigate"+palette.DimGrayClose+"  "+
			palette.BlueOpen+"enter"+palette.BlueClose+" "+
			palette.DimGrayOpen+"Select"+palette.DimGrayClose+"  "+
			palette.BlueOpen+"esc"+palette.BlueClose+" "+
			palette.DimGrayOpen+"Close"+palette.DimGrayClose,
	)
	return lines
}
