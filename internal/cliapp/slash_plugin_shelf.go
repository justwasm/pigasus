// slash_plugin_shelf.go — cycle 364: the /plugin Discover tab's shelves.
//
// 【一手】 agy 1.4.11: "Improved the `/plugin` Discover tab for long shelves:
// each shelf now shows its first 6 plugins followed by a `⋯ N more — enter to
// show` row, `Ctrl+E` expands or collapses the shelf under the cursor, and
// `[` and `]` jump to the previous or next shelf."
//
// 【strings】 the 1.4.11 binary carries the row's format ("%d more — enter to
// show" next to the "⋯ " glyph) and the three hints the panel shows for the
// new keys: "Expand Shelf", "Collapse Shelf" and "Prev/Next Shelf".
//
// The reference's shelf is a *marketplace* shelf — a curated group of
// plugins the feed itself carries. The marketplace feed the clone reads
// (internal/plugins) has no shelf of its own, so the clone's shelf is one of
// the Discover sections the 1.4.9 cycle grouped the feed into. Everything
// else — the six-plugin limit, the "more" row, the three keys — follows the
// reference.
package cliapp

import (
	"github.com/justwasm/pigasus/internal/plugins"
	"github.com/justwasm/pigasus/internal/pluginsui"
)

// pluginShelf is one shelf of the Discover tab: a name and the rows it
// shows.
type pluginShelf struct {
	// Name is the shelf's title. It is empty for the feed's single
	// unnamed shelf (the panel draws a section header only when the feed
	// groups into more than one section).
	Name string
	// Rows are the shelf's rows: its plugins plus, when the shelf is
	// collapsed and holds more than ShelfLimit plugins, the "more" row.
	Rows []pluginRow
}

// pluginDiscoverShelves is the Discover tab's shelf layout for the given
// marketplace feed.
//
// The feed groups into the four DefaultDiscoverSections; a feed that groups
// into at most one section is shown as one unnamed shelf, exactly as the
// panel has always shown it (no section header in that case).
//
// Every shelf truncates to pluginsui.ShelfLimit plugins plus a "more" row —
// the reference's rule is about the shelf, not about the header.
func (m *runMode) pluginDiscoverShelves(mkts []pluginMarketplace) []pluginShelf {
	items := toPluginRows(marketplaceItems(mkts))
	sections := pluginsui.GroupDiscoverSections(items)
	if len(sections) <= 1 {
		return []pluginShelf{m.pluginShelfRows("", items)}
	}
	out := make([]pluginShelf, 0, len(sections))
	for _, sec := range sections {
		out = append(out, m.pluginShelfRows(sec.Name, sec.Items))
	}
	return out
}

// pluginShelfRows builds one shelf's rows, honouring the shelf's expanded
// state.
func (m *runMode) pluginShelfRows(name string, items []plugins.Row) pluginShelf {
	sh := pluginShelf{Name: name}
	for _, r := range pluginsui.ShelfRows(items, m.pluginShelfIsExpanded(name)) {
		row := fromPluginRow(r.Plugin)
		row.shelf = name
		row.inShelf = true
		row.more = r.More
		row.hidden = r.Hidden
		sh.Rows = append(sh.Rows, row)
	}
	return sh
}

// pluginShelfIsExpanded reports whether the shelf shows its full list.
func (m *runMode) pluginShelfIsExpanded(name string) bool {
	return m.panel.pluginShelfExpanded[name]
}

// setPluginShelfExpanded expands or collapses a shelf.
func (m *runMode) setPluginShelfExpanded(name string, expanded bool) {
	if m.panel.pluginShelfExpanded == nil {
		m.panel.pluginShelfExpanded = map[string]bool{}
	}
	if expanded {
		m.panel.pluginShelfExpanded[name] = true
		return
	}
	delete(m.panel.pluginShelfExpanded, name)
}

// pluginDiscoverCursorRow is the Discover row under the cursor, or false
// when the cursor is not on one (the local-install row, the search-filtered
// list, an empty feed).
func (m *runMode) pluginDiscoverCursorRow() (pluginRow, bool) {
	rows := m.pluginDiscoverRows(pluginMarketplaces())
	i := m.panel.cursor
	if !m.panel.pluginSearch {
		// Row 0 is the local-install row; the entries follow it.
		i--
	}
	if i < 0 || i >= len(rows) {
		return pluginRow{}, false
	}
	return rows[i], true
}

// togglePluginShelfExpanded expands or collapses the shelf under the cursor
// (agy 1.4.11's ctrl+e).
func (m *runMode) togglePluginShelfExpanded() {
	row, ok := m.pluginDiscoverCursorRow()
	if !ok || !row.inShelf {
		return
	}
	m.setPluginShelfExpanded(row.shelf, !m.pluginShelfIsExpanded(row.shelf))
}

// jumpPluginShelf moves the cursor to the first row of the previous or the
// next shelf (agy 1.4.11's `[` and `]`).
//
// The jump wraps around at either end, like the reference's `[`/`]` in the
// /diff Per Turn tab ("wrapping around at either end", agy 1.4.8).
// 【推测】 The 1.4.11 note does not say whether the shelf jump wraps; the
// reference's other list jump does, and a shelf list is the same shape of
// control.
//
// While the cursor sits on the local-install row there is no shelf under it:
// `]` jumps to the first shelf and `[` to the last.
func (m *runMode) jumpPluginShelf(dir int) {
	shelves := m.pluginDiscoverShelves(pluginMarketplaces())
	if len(shelves) == 0 {
		return
	}
	cur := -1
	idx := 0
	for i, sh := range shelves {
		if m.panel.cursor-1 >= idx && m.panel.cursor-1 < idx+len(sh.Rows) {
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
	m.panel.cursor = row + 1
	if m.panel.cursor > m.pluginRowCount()-1 {
		m.panel.cursor = m.pluginRowCount() - 1
	}
}

// pluginShelfMoreLine renders a shelf's "⋯ N more — enter to show" row.
func pluginShelfMoreLine(isCursor bool, hidden int) string {
	return pluginsui.ShelfMoreLine(isCursor, hidden)
}
