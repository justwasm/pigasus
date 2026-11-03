// panel_rows.go — the /plugin panel's row model.
//
// The rows the panel draws come from two places: the install tree under
// the configuration home (the Installed tab) and the marketplace feed
// (the Discover tab). Both are read fresh on every draw, so the panel
// never shows a stale copy of a tree the CLI and the wrapper share.
//
// The Discover tab's rows carry two facts the plain plugin row does not:
// the shelf they belong to, and whether the row is the shelf's "⋯ N more
// — enter to show" row.

package pluginsui

import (
	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
)

// row is one row of the panel's list: a plugin, plus the shelf facts a
// Discover row carries.
type row struct {
	plugins.Row
	// shelf is the name of the Discover shelf the row belongs to, and
	// inShelf says the row is part of a shelf at all (the rows of the
	// Installed tab and of the search-filtered list are not).
	shelf   string
	inShelf bool
	// more marks a shelf's "⋯ N more — enter to show" row. Such a row
	// stands for the shelf's hidden tail: it has no plugin, and hidden
	// says how many plugins it stands for.
	more   bool
	hidden int
}

// rowsOf lifts a plain plugin list into the panel's row type.
func rowsOf(items []plugins.Row) []row {
	out := make([]row, len(items))
	for i, it := range items {
		out[i] = row{Row: it}
	}
	return out
}

// shelf is one shelf of the Discover tab: a name and the rows it shows.
type shelf struct {
	// Name is the shelf's title. It is empty for the feed's single
	// unnamed shelf (the panel draws a section header only when the feed
	// groups into more than one section).
	Name string
	// Rows are the shelf's rows: its plugins plus, when the shelf is
	// collapsed and holds more than ShelfLimit plugins, the "more" row.
	Rows []row
}

// installedRows returns the Installed tab's rows.
//
// The "enabled only" filter drops the plugins whose
// plugins.<name>.enabled is false; a read failure reads as disabled.
func (p *Panel) installedRows() []plugins.Row {
	return FilterInstalledEnabledOnly(plugins.InstalledRows(), p.enabledOnly)
}

// installedVisibleRows returns the Installed rows the tab shows under the
// current query.
//
// While the box has focus on the Installed tab the rows filter live only
// once the query is committed.
func (p *Panel) installedVisibleRows() []plugins.Row {
	all := p.installedRows()
	if p.search && p.queryCommitted {
		return FilterInstalled(all, p.query)
	}
	return all
}

// marketplaces returns the marketplace feed the Discover tab lists.
func (p *Panel) marketplaces() []plugins.Marketplace {
	return plugins.Marketplaces()
}

// discoverItems returns every marketplace entry, flattened — the count the
// tab header shows.
func (p *Panel) discoverItems(mkts []plugins.Marketplace) []plugins.Row {
	return marketplaceItems(mkts)
}

// shelves returns the Discover tab's shelf layout for the given feed.
//
// The feed groups into the four DefaultDiscoverSections; a feed that
// groups into at most one section is shown as one unnamed shelf, exactly
// as the panel has always shown it (no section header in that case).
//
// Every shelf truncates to ShelfLimit plugins plus a "more" row — the
// reference's rule is about the shelf, not about the header.
func (p *Panel) shelves(mkts []plugins.Marketplace) []shelf {
	items := marketplaceItems(mkts)
	sections := GroupDiscoverSections(items)
	if len(sections) <= 1 {
		return []shelf{p.shelfRows("", items)}
	}
	out := make([]shelf, 0, len(sections))
	for _, sec := range sections {
		out = append(out, p.shelfRows(sec.Name, sec.Items))
	}
	return out
}

// shelfRows builds one shelf's rows, honouring the shelf's expanded
// state.
func (p *Panel) shelfRows(name string, items []plugins.Row) shelf {
	sh := shelf{Name: name}
	for _, r := range ShelfRows(items, p.shelfIsExpanded(name)) {
		sh.Rows = append(sh.Rows, row{
			Row:     r.Plugin,
			shelf:   name,
			inShelf: true,
			more:    r.More,
			hidden:  r.Hidden,
		})
	}
	return sh
}

// shelfIsExpanded reports whether the shelf shows its full list.
func (p *Panel) shelfIsExpanded(name string) bool {
	if p == nil {
		return false
	}
	return p.shelfExpanded[name]
}

// setShelfExpanded expands or collapses a shelf.
func (p *Panel) setShelfExpanded(name string, expanded bool) {
	if p == nil {
		return
	}
	if p.shelfExpanded == nil {
		p.shelfExpanded = map[string]bool{}
	}
	if expanded {
		p.shelfExpanded[name] = true
		return
	}
	delete(p.shelfExpanded, name)
}

// discoverRows is the Discover row list for the current query: the
// filtered entries while the box has focus, and the shelf layout
// otherwise.
//
// The filtered list is flat: a search result set has no shelves.
func (p *Panel) discoverRows(mkts []plugins.Marketplace) []row {
	if p.search {
		return rowsOf(FilterDiscover(mkts, p.query))
	}
	var out []row
	for _, sh := range p.shelves(mkts) {
		out = append(out, sh.Rows...)
	}
	return out
}

// discoverCount is the cursor bound of the Discover tab.
//
// The unfocused tab carries its "+ Install from local directory..." row
// ahead of the entries; while the box has focus that row is hidden and
// the bound is the filtered count.
func (p *Panel) discoverCount() int {
	n := len(p.discoverRows(p.marketplaces()))
	if !p.search {
		n++ // the local-install row
	}
	return n
}

// searchCount is the cursor bound while the box has focus.
//
// On the Installed tab it returns the filtered count *only after* the
// user has committed the query (Enter). While the box has focus and the
// query has not yet been committed, the rows below stay unfiltered, so
// the bound is the full installed count.
//
// The typed-search filter composes on top of the "enabled only" filter
// rather than replacing it: a user who asked for "enabled only" and then
// typed `g` sees only the enabled rows whose name and description
// match.
func (p *Panel) searchCount() int {
	if p.tab == TabInstalled {
		base := p.installedRows()
		if p.search && p.queryCommitted {
			return len(FilterInstalled(base, p.query))
		}
		return len(base)
	}
	return p.discoverCount()
}

// RowCount is the cursor bound of the tab currently shown.
func (p *Panel) RowCount() int {
	if p == nil {
		return 0
	}
	if p.tab == TabDiscover {
		return p.discoverCount()
	}
	if p.search {
		return p.searchCount()
	}
	return len(p.installedRows())
}

// navCount is the bound the leftover list navigation uses.
//
// The panel's own ↑/↓ follow the row list it draws; the generic list
// navigation the host used to run for the leftover keys is bounded by the
// install tree instead, and that is the bound it keeps.
func (p *Panel) navCount() int {
	return len(plugins.InstalledRows())
}

// cursorFor clamps the panel cursor to a row count.
func (p *Panel) cursorFor(n int) int {
	if p.cursor < 0 || (n > 0 && p.cursor >= n) {
		return 0
	}
	if n == 0 {
		return 0
	}
	return p.cursor
}

// discoverCursorRow is the Discover row under the cursor, or false when
// the cursor is not on one (the local-install row, the search-filtered
// list, an empty feed).
func (p *Panel) discoverCursorRow() (row, bool) {
	rows := p.discoverRows(p.marketplaces())
	i := p.cursor
	if !p.search {
		// Row 0 is the local-install row; the entries follow it.
		i--
	}
	if i < 0 || i >= len(rows) {
		return row{}, false
	}
	return rows[i], true
}

// cursorShelfExpanded reports whether the shelf under the Discover cursor
// is expanded — the footer says "Collapse Shelf" then.
func (p *Panel) cursorShelfExpanded() bool {
	r, ok := p.discoverCursorRow()
	if !ok || !r.inShelf {
		return false
	}
	return p.shelfIsExpanded(r.shelf)
}
