// panel_render.go — the overlay's rows.
//
// The rows are read fresh on every draw, so a tab switch, a cursor
// move or a terminal resize shows up on the next frame. The list
// window is clamped while the rows are built, which is what keeps the
// cursor inside the catalogue after a resize.
//
// The colours come from tuilib directly: tuilib refreshes its own
// aliases when the colour scheme changes, so the bytes follow the
// scheme without the panel holding a second copy of them.

package helppanel

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/vim"
)

// Rows renders the overlay.
//
// The block is the tab bar, a newline and the active tab's body. A
// closed overlay has no rows: the host draws nothing for it.
func (p *Panel) Rows() string {
	if p == nil || !p.open {
		return ""
	}
	tabs := p.tabs()
	tab := p.tab
	if tab < 0 || tab >= len(tabs) {
		tab = 0
	}
	var b strings.Builder
	b.WriteString(tuilib.RenderTabBarTabs(tab, tabs))
	b.WriteString("\n")
	switch tab {
	case 0:
		tuilib.RenderHelpGeneral(&b, p.model(), p.tier(),
			tuilib.ShortenCwd(p.cwd()), p.email(), p.conversationID())
	case 1:
		p.renderCommands(&b)
	case 2:
		p.renderShortcuts(&b)
	case 3:
		p.renderVim(&b)
	}
	return b.String()
}

// renderCommands draws the COMMANDS tab: the whole slash-command
// catalogue, sorted, with the cursor's row highlighted.
func (p *Panel) renderCommands(b *strings.Builder) {
	b.WriteString(tuilib.HelpHeaderOpen + "Available Commands" + tuilib.HelpHeaderClose + "\n\n")
	cmds := slashcommands.Sorted()
	total := len(cmds)
	p.clampWindow(total)
	start := p.window
	visible := tuilib.HelpListVisible(total, start, p.listWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		row := tuilib.BuildCommandRow(cmds[idx], idx == p.cursor, p.width())
		b.WriteString(row + "\n")
	}
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n\n",
		tuilib.HelpDimGrayOpen, from, to, total, tuilib.HelpDimGrayClose)
	b.WriteString(tuilib.HelpKeyboardHint + "\n\n")
}

// renderShortcuts draws the SHORTCUTS tab: the keyboard binding
// catalogue, with the cursor's row highlighted.
func (p *Panel) renderShortcuts(b *strings.Builder) {
	b.WriteString(tuilib.HelpHeaderOpen + "Keyboard Shortcuts" + tuilib.HelpHeaderClose + "\n")
	b.WriteString("  " + tuilib.HelpBlueOpen + "/keybindings" + tuilib.HelpBlueClose + " " +
		tuilib.HelpDimGrayOpen + "to customize" + tuilib.HelpDimGrayClose + "\n\n")
	total := len(tuilib.HelpShortcuts)
	p.clampWindow(total)
	start := p.window
	visible := tuilib.HelpListVisible(total, start, p.listWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		b.WriteString(tuilib.BuildShortcutRow(tuilib.HelpShortcuts[idx], idx == p.cursor) + "\n")
	}
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n\n",
		tuilib.HelpDimGrayOpen, from, to, total, tuilib.HelpDimGrayClose)
	b.WriteString(tuilib.HelpKeyboardHint + "\n\n")
}

// renderVim draws the VIM tab: the modal editing catalogue, with the
// cursor's row highlighted.
func (p *Panel) renderVim(b *strings.Builder) {
	b.WriteString(tuilib.HelpHeaderOpen)
	b.WriteString(vim.HelpHeader)
	b.WriteString(tuilib.HelpHeaderClose)
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(tuilib.HelpDimGrayOpen)
	b.WriteString(vim.HelpSubtitle)
	b.WriteString(tuilib.HelpDimGrayClose)
	b.WriteString("\n\n")
	total := len(vim.HelpRows)
	p.clampWindow(total)
	start := p.window
	visible := tuilib.HelpListVisible(total, start, p.listWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		b.WriteString(tuilib.BuildShortcutRow(vim.HelpRows[idx], idx == p.cursor))
		b.WriteString("\n")
	}
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n",
		tuilib.HelpDimGrayOpen, from, to, total, tuilib.HelpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(tuilib.HelpKeyboardHint)
	b.WriteString("\n\n")
}
