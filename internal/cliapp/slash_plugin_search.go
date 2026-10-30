// slash_plugin_search.go — cycle 204: the /plugin Discover tab's search box
// and its detail block.
//
// Everything here was measured by driving Antigravity CLI 1.4.0 (the live
// reference) at 220x50 and 100x50 with tmux and diffing the panel cells; the
// probes live next to this cycle's report. The three behaviours the reference
// drives that cycle 198 left inert are:
//
//   - the search box: `space` (or any printable key) focuses it on the
//     Discover tab; the placeholder is replaced by "  Search: " + the query +
//     a reverse-video block cursor, and the list is filtered live;
//   - Discover `enter`: expands a detail block whose fields differ from the
//     Installed tab (Description / Components / Marketplace / Version, no
//     Path and no Skills);
//   - the Discover footer, which is contextual to the selected row.
//
// See docs/cycles/cycle-204/deliverable-plugin-search.md.
package cliapp

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/pluginsui"
)

// pluginSearchCursor is the block the reference draws at the end of the search
// box. The reference blinks it on a timer (both `\x1b[7m\x1b[37m \x1b[0m`
// enterPluginSearch focuses the Discover search box with an empty query. The
// cursor is parked on the first result.
//
// Cycle 212: blinkOn is set true so the first frame after focus paints the
// cursor; the subsequent pluginBlinkMsg flips it on the 530 ms cadence.
//
// Cycle 219: pluginQueryCommitted and pluginQueryAsleep are both reset to
// false so a fresh focus on the Installed tab starts in the "typing"
// phase (Enter required to commit; cursor visible).
func (m *runMode) enterPluginSearch() {
	m.panel.pluginSearch = true
	m.panel.pluginQuery = ""
	m.panel.pluginQueryCommitted = false
	m.panel.pluginQueryAsleep = false
	m.panel.cursor = 0
	m.panel.pluginBlinkOn = true
}

// exitPluginSearch leaves the search box, clears the query and resets the
// cursor to the first row — measured: `esc` while focused returns the
// placeholder, restores the "+ Install from local directory..." row and parks
// the cursor on it, while an expanded detail block stays open.
//
// Cycle 219: also clears pluginQueryCommitted and pluginQueryAsleep so a
// future focus on the Installed tab starts in the "typing" phase with the
// cursor visible.
func (m *runMode) exitPluginSearch() {
	m.panel.pluginSearch = false
	m.panel.pluginQuery = ""
	m.panel.pluginQueryCommitted = false
	m.panel.pluginQueryAsleep = false
	m.panel.cursor = 0
}

// pluginSearchBoxLine is the third /plugin line. It is the placeholder on the
// Installed tab and on an unfocused Discover tab; while the Discover search box
// has focus it becomes `  Search: ` + the query + the block cursor.
//
// Cycle 212: the block cursor blinks with the rest of the /plugin cursors
// (530 ms on, 530 ms off — see pluginBlinkMsg in slash_plugin_local_install.go).
// On an "off" tick the reverse-video span is dropped entirely, leaving the
// plain trailing space the box always reserves. The steady cursor the cycle
// 204 line emitted was a single phase of the same blink.
//
// Cycle 219: while the Installed tab's box is focused the line carries a
// trailing hint. Two phases, picked by the commit flag, with a third
// sub-state on the "committed" branch driven by pluginQueryAsleep:
//
//	uncommitted  -> " [query]▍  (enter to search)"
//	            leading space + query + reverse-video cursor block +
//	            grey-238 trailing "(enter to search)" hint.
//	            The user is composing the query; Enter commits it.
//	committed, asleep (Enter just ran, no further typing yet)
//	           -> "[query]   (type to continue searching)"
//	            no leading space, no cursor block (verified at 220x50
//	            against Antigravity CLI 1.4.0: 100 captures over 2.5 s
//	            after Enter show zero cursor-block frames), just the
//	            query + a single separator space + the grey-238
//	            "(type to continue searching)" hint. Enter ran; the
//	            legend's `enter` field flipped from "Search" to
//	            "Expand Details".
//	committed, awake (user has typed since the commit)
//	           -> " [query]▍  (type to continue searching)"
//	            the cursor returns as soon as the user types.
//
// Measured at 220x50 against Antigravity CLI 1.4.0 (probes p2.out /
// p3.out / p3-typed.out — typing more characters re-opens the cursor
// block as expected).
func (m *runMode) pluginSearchBoxLine() string {
	return pluginsui.SearchBoxLine(
		m.panel.pluginTab == panelInstalled,
		m.panel.pluginTab == panelDiscover,
		m.panel.pluginSearch,
		m.panel.pluginQueryCommitted,
		m.panel.pluginQueryAsleep,
		m.panel.pluginBlinkOn,
		m.panel.pluginQuery,
	)
}

// pluginDiscoverFilter returns the marketplace entries the Discover tab shows
// for query.
//
// The reference matches a case-insensitive substring over the *marketplace's
// name* as well as the plugin's name and description. Measured at 220x50:
//
//	'antigravity' / 'official' / 'plugins' / 'gravity'  -> all 17 rows
//	'catalog'                                            -> 0 rows
//	'go' -> 5, 'gd' -> 2, 'z' -> 1, 'x' -> 3, 'k' -> 11
//
// "gravity"/"plugins"/"official" come only from the marketplace directory name
// (antigravity-plugins-official), and "catalog" comes only from the
// marketplace's *description*, which is not searched — which pins the field
// list to exactly {marketplace name} + {plugin name} + {plugin description}.
func pluginDiscoverFilter(mkts []pluginMarketplace, query string) []pluginRow {
	var out []pluginRow
	for _, r := range pluginsui.FilterDiscover(toPluginMarketplaces(mkts), query) {
		out = append(out, fromPluginRow(r))
	}
	return out
}

// pluginInstalledFilter returns the Installed rows that match query.
func pluginInstalledFilter(rows []pluginRow, query string) []pluginRow {
	var out []pluginRow
	for _, r := range pluginsui.FilterInstalled(toPluginRows(rows), query) {
		out = append(out, fromPluginRow(r))
	}
	return out
}

// pluginDiscoverRows is the Discover row list for the current query: every
// marketplace entry (grouped into shelves when unfocused) when the box is
// unfocused, the filtered set when it has focus.
//
// Cycle 364 (agy 1.4.11): the unfocused list is the shelf layout — each
// shelf's first plugins plus its "⋯ N more — enter to show" row — so the
// list carries the more-rows the cursor can land on. The filtered list is
// flat: a search result set has no shelves.
func (m *runMode) pluginDiscoverRows(mkts []pluginMarketplace) []pluginRow {
	if m.panel.pluginSearch {
		return pluginDiscoverFilter(mkts, m.panel.pluginQuery)
	}
	var out []pluginRow
	for _, sh := range m.pluginDiscoverShelves(mkts) {
		out = append(out, sh.Rows...)
	}
	return out
}

// pluginDiscoverCount is the cursor bound of the Discover tab. The unfocused
// tab carries its "+ Install from local directory..." row ahead of the
// entries; while the box has focus that row is hidden and the bound is the
// filtered count.
func (m *runMode) pluginDiscoverCount() int {
	n := len(m.pluginDiscoverRows(pluginMarketplaces()))
	if !m.panel.pluginSearch {
		n++ // the local-install row
	}
	return n
}

// togglePluginDiscoverExpanded expands/collapses the detail block under the
// highlighted Discover row. On the unfocused tab row 0 is the local-install
// row, whose Enter opens the reference's inline path prompt (not implemented —
// see the cycle-204 report), so it is inert here.
//
// Cycle 364 (agy 1.4.11): Enter on a shelf's "⋯ N more — enter to show" row
// shows the shelf's tail instead of a detail block.
func (m *runMode) togglePluginDiscoverExpanded() {
	if !m.panel.pluginSearch && m.panel.cursor == 0 {
		return
	}
	rows := m.pluginDiscoverRows(pluginMarketplaces())
	i := m.panel.cursor
	if !m.panel.pluginSearch {
		i--
	}
	if i < 0 || i >= len(rows) {
		return
	}
	if rows[i].more {
		m.setPluginShelfExpanded(rows[i].shelf, true)
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

// handlePluginSearchKey handles every key while the Discover search box has
// focus. It reports whether it consumed the key; the caller swallows consumed
// keys and lets modified keys it did not take fall through to the global
// bindings (so ctrl+c still quits).
//
// Measured (probe p16/p18/p19): `↑/↓` move the cursor through the filtered
// list; `enter` expands the highlighted row's detail block; a printable key
// (including `space`) appends to the query and parks the cursor on the first
// result; `backspace` drops the last rune and, on an empty query, leaves search
// mode; `ctrl+u` and `esc` leave search mode.
//
// Cycle 212: `tab` keeps the box focused while cycling Installed↔Discover and
// carries the typed query across; the Installed-tab focused rendering was
// deferred.
//
// Cycle 219: when the Installed tab's box is focused and the typed query has
// not been committed, Enter commits the query (sets pluginQueryCommitted) and
// does NOT toggle the detail block — that action is owned by Enter only after
// the commit, where the legend switches to "Expand Details". Discover's
// existing Enter-behaviour (expand the detail block) is unchanged; its filter
// is always live.
//
// `←/→` still jump to the first / last filtered row, which is what they do on
// the unfocused tab.
func (m *runMode) handlePluginSearchEnter() bool {
	// Cycle 219: on the Installed tab, an un-committed query
	// commits on Enter; a committed query expands the detail block
	// like Discover does (togglePluginExpanded). Discover's
	// always-live filter means there is nothing to commit there.
	// Pressing Enter also puts the cursor to sleep — the reference
	// hides the reverse-video block until the user types again.
	if m.panel.pluginTab == panelInstalled && !m.panel.pluginQueryCommitted {
		m.panel.pluginQueryCommitted = true
		m.panel.pluginQueryAsleep = true
		m.panel.cursor = 0
		return true
	}
	if m.panel.pluginTab == panelInstalled {
		// A second Enter (already committed) re-awakens the cursor.
		m.panel.pluginQueryAsleep = false
		m.togglePluginExpanded()
		return true
	}
	m.togglePluginDiscoverExpanded()
	return true
}

func (m *runMode) handlePluginSearchNavKey(code rune) bool {
	switch code {
	case tea.KeyTab:
		// Cycle 212: keep the search box focused on the new tab.
		if m.panel.pluginTab == panelInstalled {
			m.panel.pluginTab = panelDiscover
		} else {
			m.panel.pluginTab = panelInstalled
		}
		m.panel.cursor = 0
		return true
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true
	case tea.KeyDown:
		if m.panel.cursor < m.pluginSearchCount()-1 {
			m.panel.cursor++
		}
		return true
	case tea.KeyLeft:
		m.panel.cursor = 0
		return true
	case tea.KeyRight:
		if n := m.pluginSearchCount(); n > 0 {
			m.panel.cursor = n - 1
		}
		return true
	default:
		return false
	}
}

func (m *runMode) handlePluginSearchEditKey(key tea.Key) bool {
	switch key.Code {
	case tea.KeyBackspace:
		if m.panel.pluginQuery == "" {
			m.exitPluginSearch()
			return true
		}
		r := []rune(m.panel.pluginQuery)
		m.panel.pluginQuery = string(r[:len(r)-1])
		m.panel.pluginQueryAsleep = false
		if m.panel.pluginTab == panelDiscover {
			m.panel.cursor = 0
		}
		return true
	case tea.KeySpace:
		if m.panel.pluginTab == panelInstalled && m.panel.pluginQueryCommitted {
			m.togglePluginEnabled()
			return true
		}
		m.panel.pluginQuery += " "
		m.panel.pluginQueryAsleep = false
		if m.panel.pluginTab == panelDiscover {
			m.panel.cursor = 0
		}
		return true
	}
	if key.Mod == 0 {
		if key.Text != "" {
			m.panel.pluginQuery += key.Text
			m.panel.pluginQueryAsleep = false
			if m.panel.pluginTab == panelDiscover {
				m.panel.cursor = 0
			}
		}
		return true
	}
	return false
}

// handlePluginSearchKey handles key events while the search input has focus.
func (m *runMode) handlePluginSearchKey(key tea.Key) bool {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'u' {
		m.exitPluginSearch()
		return true
	}
	if key.Code == tea.KeyEsc {
		m.exitPluginSearch()
		return true
	}
	if key.Code == tea.KeyEnter {
		return m.handlePluginSearchEnter()
	}
	if m.handlePluginSearchNavKey(key.Code) {
		return true
	}
	return m.handlePluginSearchEditKey(key)
}

// pluginSearchCount is the cursor bound while the box has focus.
//
// Cycle 219: it returns the filtered count on the Installed tab *only after*
// the user has committed the query (Enter). While the box has focus and the
// query has not yet been committed on Installed, the rows below stay
// unfiltered, so the bound is the full installed count. Discover is always
// live-filtered while focused.
func (m *runMode) pluginSearchCount() int {
	if m.panel.pluginTab == panelInstalled {
		if m.panel.pluginSearch && m.panel.pluginQueryCommitted {
			// Cycle dpl (AGY 1.2.7): the typed-search filter composes on top
			// of pluginEnabledOnly rather than replacing it. A user who
			// asked for "enabled only" and then typed `g` should see only
			// enabled rows whose name+desc matches; reverting to the full
			// installed list here would re-introduce the disabled rows the
			// customization query filtered out.
			base := installedPluginRowsFiltered(m.panel.pluginEnabledOnly)
			return len(pluginInstalledFilter(base, m.panel.pluginQuery))
		}
		return len(installedPluginRowsFiltered(m.panel.pluginEnabledOnly))
	}
	return m.pluginDiscoverCount()
}

// pluginDiscoverLegend, pluginInstalledLegend, and details are in slash_plugin_details.go
