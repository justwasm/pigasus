// panel_render.go — the /plugin panel's rows.
//
// The layout was recovered from the reference's own capture
// (/tmp/agycmp/panels/ppref.plugin.raw.txt) and re-probed key by key:
//
//	title        "\x1b[1m\x1b[39m  Plugins\x1b[0m"  (two leading spaces)
//	tab row      "  <Installed (n)>    <Discover (m)>   (tab to cycle)"
//	             the active tab is bold+underline+blue and its count is
//	             bold; the inactive tab is one grey span.
//	search box   "  Type to search plugins..." in the muted foreground
//	Installed    "> [enabled]  <name> <desc>" — the badge is green with a
//	             trailing space inside the span, so both badges occupy ten
//	             columns; the name is bold blue on the cursor row and
//	             bold default-fg otherwise; the description is dim.
//	Discover     "  ● <name> <desc>" behind a "> + Install from local
//	             directory..." row.
//
// Rows are truncated so the whole line fits terminal_width-2 columns,
// with "...": measured at 220 → 218 and 100 → 98. The cut is mid-word,
// not word-wrapped.

package pluginsui

import (
	"github.com/justwasm/pigasus/internal/features/auth"
	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
	"github.com/justwasm/pigasus/palette"
)

const (
	// panelTitleOpen is the panel title's opening span: bold, default
	// foreground.
	panelTitleOpen = "\x1b[1m\x1b[39m"
	// panelTitleClose ends the title span.
	panelTitleClose = "\x1b[0m"
	// panelErrorOpen is the red the reference paints a failed install
	// with.
	panelErrorOpen = "\x1b[38;5;166m"
	// panelErrorClose ends the red span.
	panelErrorClose = "\x1b[39m"
)

// PanelSubtleOpen is the panel's muted foreground — the selected scheme's
// subtle colour.
//
// The host's own panelGray238 reads the same value, so the two agree
// byte for byte.
var PanelSubtleOpen = palette.SubtleOpen

// PanelSubtleClose ends a muted run.
const PanelSubtleClose = "\x1b[39m"

// lineMax is the widest /plugin line the reference emits.
func lineMax(width int) int {
	if width <= 0 {
		return 1 << 20
	}
	return width - 2
}

// title is the panel's title line, with the last install error beside it.
func (p *Panel) title() string {
	t := panelTitleOpen + "  Plugins" + panelTitleClose
	if p.localError != "" {
		t += " " + panelErrorOpen + "(Error: " + p.localError + ")" + panelErrorClose
	}
	return t
}

// Lines renders the panel.
//
// The tab's body follows the title, the tab row and the search box; the
// Installed tab adds its rows, its empty note and its footer, and the
// Discover tab adds its rows and its footer.
func (p *Panel) Lines(width int) []string {
	if p == nil {
		return nil
	}
	installed := p.installedRows()
	mkts := p.marketplaces()
	discover := p.discoverItems(mkts)
	max := lineMax(width)

	visible := p.installedVisibleRows()
	headerInstalled := len(installed)
	if p.search && p.queryCommitted {
		headerInstalled = len(visible)
	}

	lines := []string{
		p.title(),
		TabHeader(p.tab, headerInstalled, len(discover)),
		SearchBoxLine(p.tab == TabInstalled, p.tab == TabDiscover,
			p.search, p.queryCommitted, p.queryAsleep, p.blinkOn, p.query),
	}

	if p.tab == TabDiscover {
		return append(lines, p.discoverLines(mkts, max)...)
	}

	cursor := p.cursorFor(len(visible))
	lines = append(lines, p.installedRowsLines(visible, cursor, max, mkts)...)
	lines = append(lines, InstalledEmptyNote(
		p.search && p.queryCommitted, p.enabledOnly, len(visible))...)
	lines = append(lines, "", InstalledLegend(p.search, p.queryCommitted))
	return lines
}

// installedRowsLines renders the Installed rows, each followed by its
// detail block when the block is open.
func (p *Panel) installedRowsLines(visible []plugins.Row, cursor, max int, mkts []plugins.Marketplace) []string {
	var lines []string
	for i, r := range visible {
		isCursor := i == cursor
		desc := plugins.DescFor(r.Name, mkts)
		lines = append(lines, InstalledLine(isCursor, pluginEnabled(r.Name), r.Name, desc, max))
		if p.expanded[r.Name] {
			lines = append(lines, InstalledDetails(r.Name, mkts)...)
		}
	}
	return lines
}

// discoverLines is the Discover tab body below its search box: the
// filtered entries each followed by their detail block when expanded, the
// contextual footer, and — while the box has focus and nothing matches —
// the reference's "No marketplace plugins match your search." row.
//
// The no-match screen is box / message / blank / blank / Keyboard, one
// more blank than a screen with rows, so the empty branch appends its own
// trailing blank.
func (p *Panel) discoverLines(mkts []plugins.Marketplace, max int) []string {
	rows := p.discoverRows(mkts)
	searching := p.search
	prompt := p.local.active
	offset := 0
	var lines []string
	if prompt {
		// The local-install row is now the prompt itself; the cursor is
		// parked there because the prompt owns every keypress.
		lines = append(lines, LocalInstallRow(true, p.local.query, p.blinkOn))
		if h := LocalInstallHint(p.local.query); h != "" {
			lines = append(lines, h)
		}
	} else if !searching {
		// The "+ Install from local directory..." row heads the unfocused
		// list only; the box's focus hides it.
		offset = 1
		lines = append(lines, LocalRow(p.cursorFor(len(rows)+1) == 0))
	}
	cursor := p.cursorFor(len(rows) + offset)
	installedSet := plugins.InstalledNames()
	if !searching {
		// The Discover list is a shelf layout — each shelf shows its
		// first plugins plus a "⋯ N more — enter to show" row. A feed
		// that groups into more than one section draws the section
		// headers; one that does not is shown as a single unnamed shelf.
		shelves := p.shelves(mkts)
		multi := len(shelves) > 1
		idx := 0
		for _, sh := range shelves {
			if multi {
				lines = append(lines, "", DiscoverSectionHeader(sh.Name))
			}
			for _, r := range sh.Rows {
				if r.more {
					lines = append(lines, ShelfMoreLine(cursor == idx+offset, r.hidden))
					idx++
					continue
				}
				lines = append(lines, DiscoverLine(cursor == idx+offset, installedSet[r.Name], r.Name, r.Desc, max))
				if p.expanded[r.Name] {
					lines = append(lines, DiscoverDetails(r.Name, mkts, max)...)
				}
				idx++
			}
		}
	} else {
		for i, r := range rows {
			lines = append(lines, DiscoverLine(cursor == i+offset, installedSet[r.Name], r.Name, r.Desc, max))
			if p.expanded[r.Name] {
				lines = append(lines, DiscoverDetails(r.Name, mkts, max)...)
			}
		}
		if len(rows) == 0 {
			lines = append(lines,
				PanelSubtleOpen+"  No marketplace plugins match your search."+PanelSubtleClose,
				"")
		}
	}
	if prompt {
		lines = append(lines, "", LocalInstallLegend())
	} else {
		lines = append(lines, "", DiscoverLegend(!searching && cursor == 0, searching, p.cursorShelfExpanded()))
	}
	return lines
}

// pluginEnabled reads the badge state from the configuration file. A read
// failure shows [disabled] rather than a stale value.
func pluginEnabled(name string) bool {
	on, err := auth.PluginEnabled(name)
	return err == nil && on
}
