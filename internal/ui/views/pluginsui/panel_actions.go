// panel_actions.go — the /plugin panel's writes.
//
// The panel owns the three writes the reference performs from the panel:
// the enabled flag, the uninstall, and the local install. The uninstall
// and the local install are the package's own (they write the
// configuration file), and the enabled flag is the configuration
// accessor's.

package pluginsui

import (
	"github.com/justwasm/pigasus/internal/features/auth"
	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
)

// installedActiveRows returns the rows the Installed tab's actions apply
// to.
//
// While the search box has focus on the Installed tab and the query is
// committed, the list is the query's result; otherwise it is the whole
// install tree.
func (p *Panel) installedActiveRows() []plugins.Row {
	rows := plugins.InstalledRows()
	if p.tab == TabInstalled && p.search && p.queryCommitted {
		return FilterInstalled(rows, p.query)
	}
	return rows
}

// toggleEnabled flips the highlighted plugin's enabled flag.
//
// The flag lives in the configuration file, so the write goes through the
// configuration accessor; a read failure leaves the flag alone.
func (p *Panel) toggleEnabled() {
	rows := p.installedActiveRows()
	i := p.cursorFor(len(rows))
	if i >= len(rows) {
		return
	}
	name := rows[i].Name
	on, err := auth.PluginEnabled(name)
	if err != nil {
		return
	}
	_ = auth.SetPluginEnabled(name, !on)
}

// toggleExpanded opens or closes the detail block under the highlighted
// Installed row.
//
// The block is keyed on the plugin, not on the cursor, so it survives
// cursor movement.
func (p *Panel) toggleExpanded() {
	rows := p.installedActiveRows()
	i := p.cursorFor(len(rows))
	if i >= len(rows) {
		return
	}
	name := rows[i].Name
	if p.expanded == nil {
		p.expanded = map[string]bool{}
	}
	if p.expanded[name] {
		delete(p.expanded, name)
		return
	}
	p.expanded[name] = true
}

// toggleDiscoverExpanded opens or closes the detail block under the
// highlighted Discover row.
//
// On the unfocused tab row 0 is the local-install row, whose Enter opens
// the inline path prompt, so it is inert here.
//
// Enter on a shelf's "⋯ N more — enter to show" row shows the shelf's
// tail instead of a detail block.
func (p *Panel) toggleDiscoverExpanded() {
	if !p.search && p.cursor == 0 {
		return
	}
	rows := p.discoverRows(p.marketplaces())
	i := p.cursor
	if !p.search {
		i--
	}
	if i < 0 || i >= len(rows) {
		return
	}
	if rows[i].more {
		p.setShelfExpanded(rows[i].shelf, true)
		return
	}
	name := rows[i].Name
	if p.expanded == nil {
		p.expanded = map[string]bool{}
	}
	if p.expanded[name] {
		delete(p.expanded, name)
		return
	}
	p.expanded[name] = true
}

// uninstallHighlighted uninstalls the highlighted plugin and reports what
// the uninstall said.
//
// The detail block of the uninstalled plugin is closed, and the message
// is the panel's title's error slot.
func (p *Panel) uninstallHighlighted() (string, error) {
	rows := plugins.InstalledRows()
	i := p.cursorFor(len(rows))
	if i >= len(rows) {
		return "", nil
	}
	name := rows[i].Name
	msg, err := UninstallFeedback(name)
	if err != nil && msg == "" {
		return "Error: " + err.Error(), err
	}
	if p.expanded != nil {
		delete(p.expanded, name)
	}
	return msg, err
}

// toggleShelfExpanded expands or collapses the shelf under the cursor.
func (p *Panel) toggleShelfExpanded() {
	r, ok := p.discoverCursorRow()
	if !ok || !r.inShelf {
		return
	}
	p.setShelfExpanded(r.shelf, !p.shelfIsExpanded(r.shelf))
}

// jumpShelf moves the cursor to the first row of the previous or the
// next shelf.
//
// The jump wraps around at either end, like the reference's '[' / ']' in
// the /diff Per Turn tab ("wrapping around at either end").
//
// While the cursor sits on the local-install row there is no shelf under
// it: ']' jumps to the first shelf and '[' to the last.
func (p *Panel) jumpShelf(dir int) {
	shelves := p.shelves(p.marketplaces())
	if len(shelves) == 0 {
		return
	}
	cur := -1
	idx := 0
	for i, sh := range shelves {
		if p.cursor-1 >= idx && p.cursor-1 < idx+len(sh.Rows) {
			cur = i
			break
		}
		idx += len(sh.Rows)
	}
	target := 0
	if cur < 0 {
		if dir < 0 {
			target = len(shelves) - 1
		}
	} else {
		target = (cur + dir + len(shelves)) % len(shelves)
	}
	row := 0
	for i := 0; i < target; i++ {
		row += len(shelves[i].Rows)
	}
	// +1 for the local-install row ahead of the entries.
	p.cursor = row + 1
	if p.cursor > p.RowCount()-1 {
		p.cursor = p.RowCount() - 1
	}
}
