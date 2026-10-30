package cliapp

import (
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/pluginsui"
	"github.com/justwasm/pigasus/palette"
)

// pluginColor253 is the xterm-253 grey REF uses for the inactive tab, the
// `[disabled]` badge, the Discover `●` bullets and the whole expanded details
// block. Emitted as a 256-colour code because the truecolour value behind it
// was not recovered; `capture-pane` normalises REF's RGB down to this index
// anyway (see the colour caveat at the top of this file).
const (
	pluginColor253C = "\x1b[39m"
)

// pluginColor253 is the selected scheme's separator colour.
var pluginColor253 = palette.SeparatorOpen

// panelPluginLines renders the /plugin picker. Layout recovered from
// /tmp/agycmp/panels/ppref.plugin.raw.txt and re-probed key-by-key in cycle
// 198 (/tmp/agycmp/probe_ref_plugin2.py, probe_ref_plugin3.py):
//
//	title        "\x1b[1m\x1b[39m  Plugins\x1b[0m"  (two leading spaces)
//	tab row      "  <Installed (n)>    <Discover (m)>   (tab to cycle)"
//	             the active tab is bold+underline+blue69 and its count is bold;
//	             the inactive tab is one grey-253 span.
//	search box   "  Type to search plugins..." grey 238
//	Installed    "> [enabled]  <name> <desc>" — the badge is green 29 with a
//	             trailing space inside the span, `[disabled]` is grey 253 with
//	             none, so both occupy ten columns; the name is bold blue on the
//	             cursor row and bold default-fg otherwise; the description is
//	             grey 241. Enter appends the five-line detail block.
//	Discover     "  ● <name> <desc>" (grey-253 bullet, bold name, grey-241
//	             description) behind a "> + Install from local directory..."
//	             row.
//
// Rows are truncated so the whole line fits terminal_width-2 columns, with
// "...": measured at 220 → 218 and 100 → 98 (/tmp/agycmp/probe_ref_width.py).
// The cut is mid-word, not word-wrapped.
//
// Esc closes silently (esc-ref-plugin-Esc.txt shows no `Exited` row).
//
// Cycle 198: m.panel.pluginTab selects the tab (panelInstalled / panelDiscover).
// The Installed tab lists the installed plugins with their enable badge; the
// Discover tab lists the marketplace entries behind the "+ Install from local
// directory..." row. See the cycle-198 report.
//
// Cycle 204 adds the Discover search box (space / a printable key focuses it,
// its text filters the list live and it carries a blinking reverse-video
// cursor in the reference), the Discover detail block `enter` opens, and the
// `←/→` first/last cursor jumps its footer announces. The box line, the legend
// and the two key paths live in slash_plugin_search.go; the details live there
// too (pluginDiscoverDetails). See docs/cycles/cycle-204/.
func (m *runMode) pluginPanelTitle() string {
	title := panelTitleOpen + "  Plugins" + panelTitleClose
	if m.panel.pluginLocalError != "" {
		title += " \x1b[38;5;166m(Error: " + m.panel.pluginLocalError + ")\x1b[39m"
	}
	return title
}

func (m *runMode) panelPluginInstalledRows(visible []pluginRow, cursor, max int, mkts []pluginMarketplace) []string {
	var lines []string
	for i, p := range visible {
		isCursor := i == cursor
		desc := pluginDescFor(p.name, mkts)
		lines = append(lines, pluginInstalledLine(isCursor, m.pluginEnabled(p.name), p.name, desc, max))
		if m.panel.pluginExpanded[p.name] {
			lines = append(lines, pluginInstalledDetails(p.name, mkts)...)
		}
	}
	return lines
}

func pluginInstalledEmptyNote(searchCommitted, enabledOnly bool, count int) []string {
	return pluginsui.InstalledEmptyNote(searchCommitted, enabledOnly, count)
}

func (m *runMode) panelPluginLines() []string {
	installed := installedPluginRowsFiltered(m.panel.pluginEnabledOnly)
	mkts := pluginMarketplaces()
	discover := marketplaceItems(mkts)
	max := m.pluginLineMax()

	title := m.pluginPanelTitle()
	visible := installed
	if m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		visible = pluginInstalledFilter(installed, m.panel.pluginQuery)
	}
	headerInstalled := len(installed)
	if m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		headerInstalled = len(visible)
	}

	lines := []string{
		title,
		pluginTabHeader(m.panel.pluginTab, headerInstalled, len(discover)),
		m.pluginSearchBoxLine(),
	}

	if m.panel.pluginTab == panelDiscover {
		return append(lines, m.panelDiscoverLines(mkts, max)...)
	}

	cursor := m.pluginCursorFor(len(visible))
	lines = append(lines, m.panelPluginInstalledRows(visible, cursor, max, mkts)...)
	lines = append(lines, pluginInstalledEmptyNote(
		m.panel.pluginSearch && m.panel.pluginQueryCommitted,
		m.panel.pluginEnabledOnly,
		len(visible))...)
	lines = append(lines, "", m.pluginInstalledLegend())
	return lines
}

// panelDiscoverLines is the Discover tab body below its search box: the
// filtered entries (cycle 204) each followed by their detail block when
// expanded, the contextual footer, and — while the box has focus and nothing
// matches — the reference's "No marketplace plugins match your search." row.
//
// Measured at 220x50 (probes p16/p19): the no-match screen is
// box / message / blank / blank / Keyboard, i.e. one more blank than a screen
// with rows, so the empty branch appends its own trailing blank.
//
// Cycle 212: the local-install prompt replaces the local row while it is
// open and (only when the typed path is invalid) appends a hint line that
// shifts the rest of the panel down by one — see p1/p2/p4.out.
func (m *runMode) panelDiscoverLines(mkts []pluginMarketplace, max int) []string {
	rows := m.pluginDiscoverRows(mkts)
	searching := m.panel.pluginSearch
	prompt := m.pluginLocalInstallActive()
	offset := 0
	var lines []string
	if prompt {
		// The local-install row is now the prompt itself; the cursor is
		// parked there because the prompt owns every keypress.
		lines = append(lines, pluginLocalInstallRow(true,
			m.panel.pluginLocalInstall.query, m.panel.pluginBlinkOn))
		if h := pluginLocalInstallHint(m.panel.pluginLocalInstall.query); h != "" {
			lines = append(lines, h)
		}
	} else if !searching {
		// The "+ Install from local directory..." row heads the unfocused
		// list only; the box's focus hides it.
		offset = 1
		lines = append(lines, pluginLocalRow(m.pluginCursorFor(len(rows)+1) == 0))
	}
	cursor := m.pluginCursorFor(len(rows) + offset)
	installedSet := installedNames()
	if !searching {
		// Cycle 364 (agy 1.4.11): the Discover list is a shelf layout —
		// each shelf shows its first plugins plus a "⋯ N more — enter to
		// show" row. A feed that groups into more than one section draws
		// the section headers; one that does not is shown as a single
		// unnamed shelf, as before.
		shelves := m.pluginDiscoverShelves(mkts)
		multi := len(shelves) > 1
		idx := 0
		for _, sh := range shelves {
			if multi {
				lines = append(lines, "", pluginsui.DiscoverSectionHeader(sh.Name))
			}
			for _, p := range sh.Rows {
				if p.more {
					lines = append(lines, pluginShelfMoreLine(cursor == idx+offset, p.hidden))
					idx++
					continue
				}
				lines = append(lines, pluginDiscoverLine(cursor == idx+offset, installedSet[p.name], p.name, p.desc, max))
				if m.panel.pluginExpanded[p.name] {
					lines = append(lines, pluginDiscoverDetails(p.name, mkts, max)...)
				}
				idx++
			}
		}
	} else {
		for i, p := range rows {
			lines = append(lines, pluginDiscoverLine(cursor == i+offset, installedSet[p.name], p.name, p.desc, max))
			if m.panel.pluginExpanded[p.name] {
				lines = append(lines, pluginDiscoverDetails(p.name, mkts, max)...)
			}
		}
		if len(rows) == 0 {
			lines = append(lines,
				panelGray238+"  No marketplace plugins match your search."+panelGray238C,
				"")
		}
	}
	if prompt {
		lines = append(lines, "", pluginLocalInstallLegend())
	} else {
		lines = append(lines, "", m.pluginDiscoverLegend(!searching && cursor == 0))
	}
	return lines
}

// The two /plugin tabs.
const (
	panelInstalled = iota
	panelDiscover
)

// pluginTabHeader is the "  Installed (4)    Discover (17)   (tab to cycle)"
// row. Only the active tab carries the bold/underline/blue styling.
func pluginTabHeader(tab, nInstalled, nDiscover int) string {
	return pluginsui.TabHeader(tab, nInstalled, nDiscover)
}

// pluginBadge is the [enabled]/[disabled] tag.
func pluginBadge(enabled bool) string {
	return pluginsui.Badge(enabled)
}

// pluginBlockedBadge is the [blocked] tag.
func pluginBlockedBadge() string {
	return pluginsui.BlockedBadge()
}

// pluginRowBadge is the badge for one Installed row.
func pluginRowBadge(enabled bool) string {
	return pluginsui.RowBadge(enabled)
}

// pluginInstalledLine renders one Installed row.
func pluginInstalledLine(isCursor, enabled bool, name, desc string, max int) string {
	return pluginsui.InstalledLine(isCursor, enabled, name, desc, max)
}

// pluginDiscoverLine renders one Discover plugin row.
func pluginDiscoverLine(isCursor, installed bool, name, desc string, max int) string {
	return pluginsui.DiscoverLine(isCursor, installed, name, desc, max)
}

// pluginLocalRow is Discover's first row, "+ Install from local directory...".
func pluginLocalRow(isCursor bool) string {
	return pluginsui.LocalRow(isCursor)
}

// pluginInstalledDetails is the block Enter opens under an Installed row.
func pluginInstalledDetails(name string, mkts []pluginMarketplace) []string {
	return pluginsui.InstalledDetails(name, toPluginMarketplaces(mkts))
}

// pluginLineMax is the widest /plugin line the reference emits.
func (m *runMode) pluginLineMax() int {
	if m.width <= 0 {
		return 1 << 20
	}
	return m.width - 2
}

// truncateTo shortens s to at most avail columns, appending "..." when it has to cut.
func truncateTo(s string, avail int) string {
	return pluginsui.TruncateTo(s, avail)
}

// pluginEnabled reads the badge state from ~/.gemini/config/config.json. A read
// failure shows [disabled] rather than a stale value.
func (m *runMode) pluginEnabled(name string) bool {
	on, err := auth.PluginEnabled(name)
	return err == nil && on
}

// pluginCursorFor clamps the panel cursor to a row count.
func (m *runMode) pluginCursorFor(n int) int {
	if m.panel.cursor < 0 || (n > 0 && m.panel.cursor >= n) {
		return 0
	}
	if n == 0 {
		return 0
	}
	return m.panel.cursor
}

// pluginRowCount is the cursor bound of the tab currently shown: the Discover
// tab carries its "+ Install from local directory..." row ahead of the
// marketplace entries while unfocused, and only the filtered entries while the
// search box has focus (cycle 204: pluginDiscoverCount). Cycle 219: the
// Installed tab's bound uses the focused-mode "un-committed = full list,
// committed = filtered list" rule too (see pluginSearchCount).
//
// Cycle dpl (AGY 1.2.7): the Installed tab's bound also honours the
// `pluginEnabledOnly` filter — disabling the last enabled row while the
// filter is active must clamp the cursor to row 0 of an empty list, not
// row 0 of the full list.

// pluginDiscoverLegend is the Discover footer.
func (m *runMode) pluginDiscoverLegend(onLocal bool) string {
	return pluginsui.DiscoverLegend(onLocal, m.panel.pluginSearch, m.pluginCursorShelfExpanded())
}

// pluginCursorShelfExpanded reports whether the shelf under the Discover
// cursor is expanded — the footer says "Collapse Shelf" then.
func (m *runMode) pluginCursorShelfExpanded() bool {
	row, ok := m.pluginDiscoverCursorRow()
	if !ok || !row.inShelf {
		return false
	}
	return m.pluginShelfIsExpanded(row.shelf)
}

// pluginInstalledLegend is the Installed tab footer.
func (m *runMode) pluginInstalledLegend() string {
	return pluginsui.InstalledLegend(m.panel.pluginSearch, m.panel.pluginQueryCommitted)
}

// pluginDiscoverDetails is the block Discover `enter` opens under a row.
func pluginDiscoverDetails(name string, mkts []pluginMarketplace, max int) []string {
	return pluginsui.DiscoverDetails(name, toPluginMarketplaces(mkts), max)
}

// pluginWrappedDetail renders a labelled detail line whose value is wrapped.
func pluginWrappedDetail(label, value string, max int) []string {
	return pluginsui.WrappedDetail(label, value, max)
}

// pluginComponents is the Discover `Components:` value.
func pluginComponents(name string, mkts []pluginMarketplace) string {
	return pluginsui.Components(name, toPluginMarketplaces(mkts))
}

// installedPluginRowsFiltered returns the rows the Installed tab renders.
func installedPluginRowsFiltered(enabledOnly bool) []pluginRow {
	all := installedPluginRows()
	rows := make([]pluginRow, 0, len(all))
	for _, r := range pluginsui.FilterInstalledEnabledOnly(toPluginRows(all), enabledOnly) {
		rows = append(rows, fromPluginRow(r))
	}
	return rows
}

func validatePluginID(id string) error {
	return pluginsui.ValidatePluginID(id)
}

func uninstallPlugin(id string) error {
	return pluginsui.UninstallPlugin(id)
}

func uninstallFeedback(id string) (string, error) {
	return pluginsui.UninstallFeedback(id)
}

func (m *runMode) pluginRowCount() int {
	if m.panel.pluginTab == panelDiscover {
		return m.pluginDiscoverCount()
	}
	if m.panel.pluginSearch {
		return m.pluginSearchCount()
	}
	return len(installedPluginRowsFiltered(m.panel.pluginEnabledOnly))
}

func (m *runMode) togglePluginEnabled() {
	rows := m.installedActiveRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		return
	}
	name := rows[i].name
	on, err := auth.PluginEnabled(name)
	if err != nil {
		return
	}
	_ = auth.SetPluginEnabled(name, !on)
}

func (m *runMode) uninstallHighlightedPlugin() (string, error) {
	rows := installedPluginRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		return "", nil
	}
	name := rows[i].name
	msg, err := uninstallFeedback(name)
	if err != nil && msg == "" {
		return "Error: " + err.Error(), err
	}
	if m.panel.pluginExpanded != nil {
		delete(m.panel.pluginExpanded, name)
	}
	return msg, err
}

func (m *runMode) togglePluginExpanded() {
	rows := m.installedActiveRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		return
	}
	name := rows[i].name
	if m.panel.pluginExpanded == nil {
		m.panel.pluginExpanded = map[string]bool{}
	}
	if m.panel.pluginExpanded[name] {
		delete(m.panel.pluginExpanded, name)
		return
	}
	m.panel.pluginExpanded[name] = true
}

func (m *runMode) installedActiveRows() []pluginRow {
	rows := installedPluginRows()
	if m.panel.pluginTab == panelInstalled &&
		m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		return pluginInstalledFilter(rows, m.panel.pluginQuery)
	}
	return rows
}
