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
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
)

// pluginSearchCursor is the block the reference draws at the end of the search
// box. The reference blinks it on a timer (both `\x1b[7m\x1b[37m \x1b[0m`
// and nothing were captured while idle); the clone renders it steadily, which
// is one of the two phases the reference itself emits.
const pluginSearchCursor = "\x1b[7m\x1b[37m \x1b[0m"

// pluginSearchPrefix is the grey label that replaces the placeholder while the
// box has focus. The trailing space is inside the grey span; the reference then
// emits one more plain space before the query.
const pluginSearchPrefix = "  Search: "

// pluginInstalledSearchHint* are the trailing grey-238 hints the reference
// paints after the cursor while the Installed tab's box is focused.
// The un-committed hint says "(enter to search)"; once Enter commits the
// query the reference swaps it for "(type to continue searching)" — the
// user is now in live-filter mode and the text explains how to keep
// narrowing the list.
//
// Measured at 220x50 (cycle-219 p2.out / p3.out): each hint is wrapped
// in its own grey-238 SGR span that re-opens after the cursor block and
// closes right before the legend row. The plain-text versions
// (pluginInstalledSearchHint / pluginInstalledSearchHintCommitted) are
// kept around for use sites that don't carry SGR (none currently — the
// only renderer goes through the *_Span versions).
const (
	pluginInstalledSearchHint              = "  (enter to search)"
	pluginInstalledSearchHintCommitted     = "  (type to continue searching)"
	pluginInstalledSearchHintSpan          = panelGray238 + pluginInstalledSearchHint + panelGray238C
	pluginInstalledSearchHintCommittedSpan = panelGray238 + pluginInstalledSearchHintCommitted + panelGray238C
)

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
	if m.panel.pluginTab == panelInstalled && m.panel.pluginSearch {
		if m.panel.pluginQueryCommitted && m.panel.pluginQueryAsleep {
			// Committed + asleep: drop the leading space and the cursor
			// block. The trailing plain space sits between the query
			// and the hint's own 2-space leading — three visible
			// spaces of separator, which matches the reference's
			// measured shape.
			return panelGray238 + pluginSearchPrefix + panelGray238C +
				m.panel.pluginQuery + " " + pluginInstalledSearchHintCommittedSpan
		}
		body := " " + m.panel.pluginQuery
		if m.panel.pluginBlinkOn {
			body += pluginCursorSpan
		} else {
			body += " "
		}
		// Uncommitted OR committed+awake: the cursor block is visible
		// (and blinks), then the (matched) hint trails it. The hint
		// re-opens the grey-238 span after the cursor block — matches
		// the reference's measured shape (probe p2.out / p3-typed.out).
		if m.panel.pluginQueryCommitted {
			body += pluginInstalledSearchHintCommittedSpan
		} else {
			body += pluginInstalledSearchHintSpan
		}
		return panelGray238 + pluginSearchPrefix + panelGray238C + body
	}
	if m.panel.pluginTab == panelDiscover && m.panel.pluginSearch {
		if m.panel.pluginBlinkOn {
			return panelGray238 + pluginSearchPrefix + panelGray238C + " " +
				m.panel.pluginQuery + pluginCursorSpan
		}
		return panelGray238 + pluginSearchPrefix + panelGray238C + " " +
			m.panel.pluginQuery + " "
	}
	return panelGray238 + "  Type to search plugins..." + panelGray238C
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
	if query == "" {
		return marketplaceItems(mkts)
	}
	q := strings.ToLower(query)
	var out []pluginRow
	for _, mk := range mkts {
		prefix := strings.ToLower(mk.name) + " "
		for _, p := range mk.items {
			if strings.Contains(prefix+strings.ToLower(p.name+" "+p.desc), q) {
				out = append(out, p)
			}
		}
	}
	return out
}

// pluginInstalledFilter returns the Installed rows that match query.
//
// Cycle 219: the field set is the same case-insensitive substring search the
// Installed description line uses — plugin name and description only — so a
// query like "g" matches google-antigravity-sdk on the reference machine. The
// empty query returns the input unchanged so the cycle-198 unfocused render
// keeps working.
func pluginInstalledFilter(rows []pluginRow, query string) []pluginRow {
	if query == "" {
		return rows
	}
	q := strings.ToLower(query)
	var out []pluginRow
	for _, p := range rows {
		if strings.Contains(strings.ToLower(p.name+" "+p.desc), q) {
			out = append(out, p)
		}
	}
	return out
}

// pluginDiscoverRows is the Discover row list for the current query: every
// marketplace entry when the box is unfocused, the filtered set when it has
// focus.
func (m *runMode) pluginDiscoverRows(mkts []pluginMarketplace) []pluginRow {
	if m.panel.pluginSearch {
		return pluginDiscoverFilter(mkts, m.panel.pluginQuery)
	}
	return marketplaceItems(mkts)
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
func (m *runMode) handlePluginSearchKey(key tea.Key) bool {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'u' {
		m.exitPluginSearch()
		return true
	}
	switch key.Code {
	case tea.KeyEsc:
		m.exitPluginSearch()
		return true
	case tea.KeyTab:
		// Cycle 212: keep the search box focused on the new tab. The
		// reference cycles Installed↔Discover without leaving search and
		// carries the query across (probed at 220x50: pressing Tab from a
		// focused Discover "g" lands on Installed with the same "g" still
		// in the box and `enter Search` in the legend).
		if m.panel.pluginTab == panelInstalled {
			m.panel.pluginTab = panelDiscover
		} else {
			m.panel.pluginTab = panelInstalled
		}
		m.panel.cursor = 0
		return true
	case tea.KeyEnter:
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
	case tea.KeyBackspace:
		if m.panel.pluginQuery == "" {
			m.exitPluginSearch()
			return true
		}
		r := []rune(m.panel.pluginQuery)
		m.panel.pluginQuery = string(r[:len(r)-1])
		// Cycle 219: backspacing back to an empty query keeps the
		// commit state intact on the Installed tab so the user can
		// keep iterating without losing the filter; on Discover the
		// filter is always live so the cursor re-park to row 0 still
		// makes sense. Backspacing counts as a typed character, which
		// wakes the cursor back up.
		m.panel.pluginQueryAsleep = false
		if m.panel.pluginTab == panelDiscover {
			m.panel.cursor = 0
		}
		return true
	case tea.KeySpace:
		// bubbletea reports a literal space as Code=KeySpace (== ' ') with
		// Text=" "; handle it explicitly so a caller that leaves Text empty
		// still inserts the space the reference does.
		//
		// Cycle psif (AGY 1.3.3 changelog line 46): once the typed query is
		// committed (Enter), Space no longer appends a space to the query —
		// it toggles the highlighted row's enabled state, matching the
		// unfocused-Space behaviour and the changelog description. In every
		// other phase (Typing on Installed, anywhere on Discover) Space is
		// just another printable character that appends a space to the
		// query and (only on Discover) re-parks the cursor on the first
		// filtered result.
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
		// A printable key (KeySpace included: its Text is " ") seeds the
		// query. On the Discover tab the filter is live so the cursor
		// re-parks on the first result; on the Installed tab (uncommitted)
		// the rows below stay unchanged, so the cursor keeps its position
		// while the user composes the query. Any printable key wakes the
		// cursor back up after a commit.
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

// pluginDiscoverLegend is the Discover footer. Measured: it names the
// local-install action only while that row is selected, and `esc` becomes
// "Exit Search" while the search box has focus.
func (m *runMode) pluginDiscoverLegend(onLocal bool) string {
	esc := "Exit"
	if m.panel.pluginSearch {
		esc = "Exit Search"
	}
	args := []string{"↑/↓", "Navigate", "←/→", "Page", "tab", "Cycle Tabs"}
	if onLocal {
		args = append(args, "enter / ctrl+s", "Enter Local Path")
	} else {
		args = append(args, "enter", "Expand Details", "ctrl+s", "Install")
	}
	args = append(args, "esc", esc)
	return panelKeyboardHint(args...)
}

// pluginInstalledLegend is the Installed tab footer.
//
// Cycle 219: while the box has focus and the typed query has not yet been
// committed (Enter), the legend names `enter` as `Search` (commit) instead
// of `Expand Details`. Once Enter commits the query, the legend switches to
// the cycle-198 default (Enter expands the highlighted row's detail block).
// `esc` becomes "Exit Search" while the box has focus — same shape the
// Discover tab uses — and the ctrl+s / space / tab bindings are unchanged.
func (m *runMode) pluginInstalledLegend() string {
	esc := "Exit"
	enter := "Expand Details"
	if m.panel.pluginSearch {
		esc = "Exit Search"
		if !m.panel.pluginQueryCommitted {
			enter = "Search"
		}
	}
	// agy 1.4.3's Installed footer does not advertise the `/`
	// enabled-only toggle (it still works, but the reference omits it
	// from the legend even at 200 columns). Cycle dpl added the
	// `/ Show Enabled Only` pair from the 1.2.7 changelog; the live
	// 1.4.3 capture is the authority, so the legend stops after
	// `space Toggle Enable`.
	return panelKeyboardHint("↑/↓", "Navigate", "←/→", "Page",
		"tab", "Cycle Tabs", "enter", enter,
		"space", "Toggle Enable",
		"ctrl+s", "Uninstall", "esc", esc)
}

// pluginDiscoverDetails is the block Discover `enter` opens under a row. It
// differs from the Installed block: no `Path:`, but a `Components:` field,
// an `Agents:` field, and the Description is word-wrapped.
//
// Measured at 220x50 for data-agent-kit-plugin and at 100x50:
//
//	'     \x1b[38;2;238;232;213m Description: <first chunk>\x1b[39m'
//	'                  \x1b[38;2;238;232;213m <continuation>\x1b[39m'
//	'     \x1b[38;2;238;232;213m Components:  <value>\x1b[39m'
//	'     \x1b[38;2;238;232;213m Agents:      <name>, <name>, ...\x1b[39m'
//	'     \x1b[38;2;238;232;213m Marketplace: antigravity-plugins-official\x1b[39m'
//	'     \x1b[38;2;238;232;213m Version:     1.1.0\x1b[39m'
//
// The five-space indent and the 13-column label field are the same as the
// Installed block, so the value column is 19; continuation lines are indented
// 18 spaces plus a plain space inside the grey span (also column 19).
//
// Cycle 234: AGY 1.3.6 changelog line 13 fixes the Discover preview listing
// every subagent declared via the plugin's root `agents.json`. The agents
// list comes from the unpacked cache path
// `<antigravity home>/cache/marketplaces/<mkt>/plugins/<name>/<version>/`,
// mirroring `Components:` — when the plugin has not been unpacked yet the
// line reads `loading...` instead of `-` so the user sees the same in-flight
// state the Installed tab's Components field already shows.
func pluginDiscoverDetails(name string, mkts []pluginMarketplace, max int) []string {
	out := pluginWrappedDetail("Description:", pluginDescFor(name, mkts), max)
	out = append(out, pluginDetailLine("Components:", pluginComponents(name, mkts)))
	out = append(out, pluginDetailLine("Agents:", pluginUnpackedAgents(name, mkts)))
	out = append(out, pluginDetailLine("Marketplace:", marketplaceOf(name, mkts)))
	out = append(out, pluginDetailLine("Version:", marketplaceVersion(name, mkts)))
	return out
}

// pluginWrappedDetail renders a labelled detail line whose value is wrapped. A
// wrapped line is at most `max` columns: the label prefix is 19 columns
// (5 + 1 + a 13-wide label) so the value wraps at max-19. Measured: 220 -> 199
// and 100 -> 79 (data-agent-kit's 397-char description breaks at exactly those
// widths; width 78 does not reproduce the reference's line breaks).
func pluginWrappedDetail(label, value string, max int) []string {
	width := max - 19
	if width < 1 {
		width = 1
	}
	chunks := wrapWords(value, width)
	out := []string{pluginDetailLine(label, chunks[0])}
	for _, c := range chunks[1:] {
		out = append(out, strings.Repeat(" ", 18)+pluginColor253+" "+c+pluginColor253C)
	}
	return out
}

// wrapWords is a greedy word-wrap on single spaces. A word longer than width is
// emitted whole (the reference does not hyphenate). It always returns at least
// one element, so an empty input yields [""] — the /plugin Discover detail block
// relies on that.
//
// This is the single shared wrapper for the whole panel package: cycle 207 also
// needs word wrapping for the /usage group note (see slash_viewer_panels.go) and
// its independently written version was folded into this one at merge time.
func wrapWords(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	words := strings.Split(s, " ")
	var out []string
	cur := ""
	for _, w := range words {
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= width:
			cur += " " + w
		default:
			out = append(out, cur)
			cur = w
		}
	}
	return append(out, cur)
}

// pluginComponents is the Discover `Components:` value. The reference fills it
// in once it has unpacked the plugin into
// <antigravity home>/cache/marketplaces/<mkt>/plugins/<name>/<version>/; until
// then it reads "loading...". Measured: gcalendar (unpacked, has a skills/
// directory) -> "skills"; data-agent-kit-plugin (not yet unpacked) -> "loading...".
//
// The clone never downloads, so it derives the value from whatever is already
// in that cache and falls back to "loading..." otherwise.
func pluginComponents(name string, mkts []pluginMarketplace) string {
	dir := filepath.Join(auth.AntigravityConfigHome(), "cache", "marketplaces",
		marketplaceOf(name, mkts), "plugins", name, marketplaceVersion(name, mkts))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "loading..."
	}
	var comps []string
	for _, e := range entries {
		if e.IsDir() {
			comps = append(comps, e.Name())
		}
	}
	sort.Strings(comps)
	if len(comps) == 0 {
		return "loading..."
	}
	return strings.Join(comps, ", ")
}
