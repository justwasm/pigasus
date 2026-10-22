package main

import (
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// pluginColor253 is the xterm-253 grey REF uses for the inactive tab, the
// `[disabled]` badge, the Discover `●` bullets and the whole expanded details
// block. Emitted as a 256-colour code because the truecolour value behind it
// was not recovered; `capture-pane` normalises REF's RGB down to this index
// anyway (see the colour caveat at the top of this file).
const (
	pluginColor253  = "\x1b[38;2;238;232;213m"
	pluginColor253C = "\x1b[39m"
)

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
func (m *runMode) panelPluginLines() []string {
	// Cycle dpl (AGY 1.2.7): installed honours the "enabled-only
	// customization query" toggle (pluginEnabledOnly) before the search
	// filter narrows further. The filter only lives in panel state, so a
	// fresh panel re-derives the full list — that is what makes the
	// disabled rows reappear after panel close.
	installed := installedPluginRowsFiltered(m.panel.pluginEnabledOnly)
	mkts := pluginMarketplaces()
	discover := marketplaceItems(mkts)
	max := m.pluginLineMax()

	title := panelTitleOpen + "  Plugins" + panelTitleClose
	if m.panel.pluginLocalError != "" {
		title = panelTitleOpen + "  Plugins" + panelTitleClose +
			" \x1b[38;5;166m(Error: " + m.panel.pluginLocalError + ")\x1b[39m"
	}
	lines := []string{title}
	lines = append(lines, pluginTabHeader(m.panel.pluginTab, len(installed), len(discover)))
	lines = append(lines, m.pluginSearchBoxLine())

	if m.panel.pluginTab == panelDiscover {
		return append(lines, m.panelDiscoverLines(mkts, max)...)
	}

	// Cycle 219: when the Installed tab's box has focus and the typed
	// query has not yet been committed (Enter), the rows below stay
	// unfiltered — the user is still composing the query. After Enter,
	// the rows filter live.
	visible := installed
	if m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		visible = pluginInstalledFilter(installed, m.panel.pluginQuery)
	}
	// The Installed count in the tab header reflects the filtered set
	// once the query has been committed — that is what the cycle-204
	// Discover header does. Before the commit the count is the full
	// installed total.
	headerInstalled := len(installed)
	if m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		headerInstalled = len(visible)
	}
	lines = []string{title}
	lines = append(lines, pluginTabHeader(m.panel.pluginTab, headerInstalled, len(discover)))
	lines = append(lines, m.pluginSearchBoxLine())

	if m.panel.pluginTab == panelDiscover {
		return append(lines, m.panelDiscoverLines(mkts, max)...)
	}

	cursor := m.pluginCursorFor(len(visible))
	for i, p := range visible {
		isCursor := i == cursor
		desc := pluginDescFor(p.name, mkts)
		lines = append(lines, pluginInstalledLine(isCursor, m.pluginEnabled(p.name), p.name, desc, max))
		if m.panel.pluginExpanded[p.name] {
			lines = append(lines, pluginInstalledDetails(p.name, mkts)...)
		}
	}
	switch {
	case m.panel.pluginSearch && m.panel.pluginQueryCommitted && len(visible) == 0:
		// Cycle 219: typed-search yielded no matches. The empty-state
		// line points at the live filter so the user knows how to
		// recover (Backspace / Esc clears the query).
		lines = append(lines,
			panelGray238+"  No installed plugins match your search."+panelGray238C,
			"")
	case m.panel.pluginEnabledOnly && len(visible) == 0:
		// Cycle dpl: an enabled-only customization query yielded no
		// matches (the user just disabled the last enabled row). The
		// reference shows this row instead of an empty list so the
		// user knows the disabled rows are still on disk — pressing
		// `/` once more re-toggles the filter.
		lines = append(lines,
			panelGray238+"  No enabled plugins — press `/` to show all."+panelGray238C,
			"")
	}
	lines = append(lines, "",
		m.pluginInstalledLegend())
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
	for i, p := range rows {
		lines = append(lines, pluginDiscoverLine(cursor == i+offset, installedSet[p.name], p.name, p.desc, max))
		if m.panel.pluginExpanded[p.name] {
			lines = append(lines, pluginDiscoverDetails(p.name, mkts, max)...)
		}
	}
	if searching && len(rows) == 0 {
		lines = append(lines,
			panelGray238+"  No marketplace plugins match your search."+panelGray238C,
			"")
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
	return "  " +
		pluginTabLabel("Installed", nInstalled, tab == panelInstalled) + "    " +
		pluginTabLabel("Discover", nDiscover, tab == panelDiscover) + " " +
		pluginColor253 + "  (tab to cycle)" + pluginColor253C
}

// pluginTabLabel renders one tab: grey-253 "Label (n)" when inactive, else
// bold+underline blue with a bold count (the reference keeps the underline on
// the space between the label and the count: `\x1b[0;4m`).
func pluginTabLabel(label string, n int, active bool) string {
	count := "(" + strconv.Itoa(n) + ")"
	if !active {
		return pluginColor253 + label + " " + count + pluginColor253C
	}
	return "\x1b[1;4m" + cBlueOpen + label + "\x1b[0;4m" + cBlueOpen + " " +
		"\x1b[1m" + count + "\x1b[0m"
}

// pluginBadge is the [enabled]/[disabled] tag. The ten-column width is fixed
// by keeping the trailing space inside the green span for [enabled] and
// leaving [disabled] a plain grey span.
func pluginBadge(enabled bool) string {
	if enabled {
		return "\x1b[38;2;133;153;0m[enabled] \x1b[39m"
	}
	return pluginColor253 + "[disabled]" + pluginColor253C
}

// pluginBlockedBadge is the third tag, rendered when the global plugin gate
// is on (AGY_CLI_DISABLE_PLUGINS env var or config.json's
// disableAllPlugins boolean). It is the AGY 1.2.16 changelog close-gap:
// /plugin previously showed "enabled" or "disabled" without indicating
// the row was also blocked by the global kill switch. The colour is
// 166 — the same red-orange the rest of the TUI uses for "error /
// blocked" rows (colors.go: cToolBulletErrOpen) — and the tag keeps the
// ten-column width the cycle-198 badge pins ("[blocked] " with a
// trailing space inside the span).
func pluginBlockedBadge() string {
	return "\x1b[38;5;166m[blocked] \x1b[39m"
}

// pluginRowState is the effective status string for one Installed row.
// The global gate wins: when auth.PluginsBlocked() is true every row
// reports "blocked" regardless of its per-plugin enabled flag. The
// panel rendering maps these three words to the matching badge.
func pluginRowState(enabled bool) string {
	if auth.PluginsBlocked() {
		return "blocked"
	}
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// pluginRowBadge is the badge for one Installed row, picking between the
// three cycle-235 states by the value of pluginRowState. It is the
// single point where "blocked" joins the existing "[enabled]" /
// "[disabled]" pair, so any future fourth state only needs to grow this
// switch.
func pluginRowBadge(enabled bool) string {
	switch pluginRowState(enabled) {
	case "blocked":
		return pluginBlockedBadge()
	case "enabled":
		return pluginBadge(true)
	default:
		return pluginBadge(false)
	}
}

// pluginInstalledLine renders one Installed row. `max` is the widest the whole
// line may be (terminal width − 2).
//
// Cycle 235: the badge comes from pluginRowBadge rather than pluginBadge
// directly, so a row that would otherwise say "[enabled]" / "[disabled]"
// gets the new "[blocked]" red-orange tag when the global plugin gate is
// on (env var or config.json's disableAllPlugins). The rest of the row
// layout is unchanged.
func pluginInstalledLine(isCursor, enabled bool, name, desc string, max int) string {
	// "  " + badge(10) + " " + name + " "
	desc = truncateTo(desc, max-(14+len(name)))
	badge := pluginRowBadge(enabled)
	if isCursor {
		return cBlueOpen + "> " + badge + " " +
			"\x1b[1m" + cBlueOpen + name + "\x1b[0m" + " " +
			cDimGrayOpen + desc + cDimGrayClose
	}
	return "  " + badge + " " +
		"\x1b[1m" + name + "\x1b[0m" + " " +
		cDimGrayOpen + desc + cDimGrayClose
}

// pluginDiscoverLine renders one Discover plugin row. The bullet is green 29
// when the plugin is installed and grey 253 otherwise — it tracks *installed*,
// not enabled (probed: disabling chrome-devtools-plugin on the Installed tab
// leaves its Discover bullet green). Only the `> ` prefix and the name change
// colour on the cursor row.
func pluginDiscoverLine(isCursor, installed bool, name, desc string, max int) string {
	desc = truncateTo(desc, max-(5+len(name)))
	bulletColor := pluginColor253
	if installed {
		bulletColor = "\x1b[38;2;133;153;0m"
	}
	bullet := bulletColor + "●" + pluginColor253C
	if isCursor {
		return cBlueOpen + "> " + bullet + " " +
			"\x1b[1m" + cBlueOpen + name + "\x1b[0m" + " " +
			cDimGrayOpen + desc + cDimGrayClose
	}
	return "  " + bullet + " " +
		"\x1b[1m" + name + "\x1b[0m" + " " +
		cDimGrayOpen + desc + cDimGrayClose
}

// pluginLocalRow is Discover's first row, "+ Install from local directory...".
// The legend's "enter / ctrl+s Enter Local Path" refers to it; the path prompt
// itself is not implemented (see the cycle-198 report).
func pluginLocalRow(isCursor bool) string {
	text := "\x1b[1m+ Install from local directory...\x1b[0m"
	if isCursor {
		return cBlueOpen + "> " + text
	}
	return "  " + text
}

// pluginInstalledDetails is the block Enter opens under an Installed row: six
// grey-253 lines indented five spaces, each label left-justified to 13 columns
// (so "Description:" gets one trailing space, "Path:" eight).
//
// Cycle 234: a sixth `Agents:` line is appended after `Skills:` so the
// detail block surfaces every subagent declared via the plugin's root
// `agents.json` (AGY 1.3.6 changelog line 13 + 35). When the plugin has no
// agents, the value is "-" — the same convention `Version:` uses for the
// missing manifest field — so the row count stays stable.
func pluginInstalledDetails(name string, mkts []pluginMarketplace) []string {
	man := readPluginManifest(name)
	skills := strings.Join(pluginSkills(name), ", ")
	agents := strings.Join(pluginAgents(name), ", ")
	if agents == "" {
		agents = "-"
	}
	version := man.Version
	if version == "" {
		version = "-"
	}
	return []string{
		pluginDetailLine("Description:", pluginDescFor(name, mkts)),
		pluginDetailLine("Skills:", skills),
		pluginDetailLine("Agents:", agents),
		pluginDetailLine("Marketplace:", marketplaceOf(name, mkts)),
		pluginDetailLine("Path:", pluginDir(name)),
		pluginDetailLine("Version:", version),
	}
}

// pluginDetailLine formats one detail row: five plain spaces, then the whole
// text in grey 253 (the leading space is inside the span).
func pluginDetailLine(label, value string) string {
	return "     " + pluginColor253 + " " + padRight(label, 13) + value + pluginColor253C
}

// padRight left-justifies s in a field of width n (no truncation).
func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// pluginLineMax is the widest /plugin line the reference emits: terminal width
// minus two (measured 220 → 218, 100 → 98). Before the first WindowSizeMsg
// (and in unit tests) the width is unknown, so return an effectively unbounded
// value.
func (m *runMode) pluginLineMax() int {
	if m.width <= 0 {
		return 1 << 20
	}
	return m.width - 2
}

// truncateTo shortens s to at most avail columns, appending "..." when it has
// to cut. avail below four columns is treated as "nothing fits".
func truncateTo(s string, avail int) string {
	if avail < 4 || len(s) <= avail {
		return s
	}
	return s[:avail-3] + "..."
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
// keep it on a row index that no longer exists.
func (m *runMode) pluginRowCount() int {
	if m.panel.pluginTab == panelDiscover {
		return m.pluginDiscoverCount()
	}
	if m.panel.pluginSearch {
		return m.pluginSearchCount()
	}
	return len(installedPluginRowsFiltered(m.panel.pluginEnabledOnly))
}

// togglePluginEnabled flips the highlighted Installed row's
// plugins.<name>.enabled flag in ~/.gemini/config/config.json (probed: the
// toggle persists across launches; the badge re-reads the flag each frame).
//
// Cycle psif: the toggle acts on the row the cursor visually points at.
// That is the full installed list while the box is unfocused, and the
// filter result once the typed query has been committed (Enter) — see
// installedActiveRows.
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

// uninstallHighlightedPlugin removes the plugin under the cursor on the
// Installed tab. It is the ctrl+s handler — the legend has listed the
// action since cycle 198, but this commit is the first to wire the
// dispatcher to a real path.
//
// Steps:
//
//  1. Read the row list (the cycle-198 reader; the disabled-filter and
//     the typed-search filter compose on top of it but a ctrl+s on a
//     visible row targets that row directly — so we read the same set
//     the cursor is pointing at, not the unfiltered disk scan).
//  2. Resolve the highlighted name.
//  3. Call uninstallFeedback (which runs validatePluginID first). The
//     return string is the message we paint in the panel title — empty
//     on success means the title reverts to its cycle-198 form.
//
// On success the directory disappears, config.json's row is cleared,
// and the next frame's installedPluginRowsFiltered call produces a
// shorter list; the cursor is clamped to row 0 by pluginCursorFor.
func (m *runMode) uninstallHighlightedPlugin() (string, error) {
	rows := installedPluginRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		// Cursor lands in an empty list; the keystroke is a no-op.
		return "", nil
	}
	name := rows[i].name
	msg, err := uninstallFeedback(name)
	if err != nil && msg == "" {
		// Defensive: uninstallFeedback always returns a non-empty
		// message on error. If it does not, surface the bare error so
		// the panel still says *something* went wrong.
		return "Error: " + err.Error(), err
	}
	// Drop any cached detail-block expansion so the next render does
	// not leave a stale "Description:" / "Skills:" block under a row
	// that no longer exists.
	if m.panel.pluginExpanded != nil {
		delete(m.panel.pluginExpanded, name)
	}
	return msg, err
}

// togglePluginExpanded shows/hides the detail block under the highlighted
// Installed row. Expansion is tracked per plugin name, not by cursor index:
// the reference leaves a block open when the cursor moves away (probed on the
// Discover tab, where the cursor jumped back to row 0 with a block still
// open).
//
// Cycle psif: same active-rows rule as togglePluginEnabled — once a
// focused-mode commit has narrowed the list, Enter on a committed
// Installed box opens the block under the filtered row the cursor is
// parked on, not the same-index row of the full list.
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

// installedActiveRows is the Installed-tab row list the cursor
// currently indexes. With no focused-mode commit it is the full
// installed list; once the typed query has been committed (Enter) it
// is the filter result, so a Space / Enter on the focused state acts
// on the row the cursor visually points at.
//
// Cycle psif: previously both togglePluginEnabled and
// togglePluginExpanded read installedPluginRows() unconditionally,
// which indexed the cursor into the *full* list even after a commit
// narrowed the visible rows. With more than one installed plugin the
// cursor and the action would disagree — Space would flip the wrong
// row's enabled flag, Enter would open the wrong row's detail block.
// The fix is one helper that both callers share.
func (m *runMode) installedActiveRows() []pluginRow {
	rows := installedPluginRows()
	if m.panel.pluginTab == panelInstalled &&
		m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		return pluginInstalledFilter(rows, m.panel.pluginQuery)
	}
	return rows
}


