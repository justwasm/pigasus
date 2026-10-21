package main

// slash_cycle219_test.go — cycle 219: the Installed tab's focused-mode
// rendering.
//
// Every expectation below is pinned to the live reference (Antigravity CLI
// 1.4.0), driven at 220x50 through a private tmux socket; the probe
// transcripts are /private/var/folders/.../opencode/agy219/p*.out and the
// cycle report is docs/cycles/cycle-219/deliverable-plugin-installed-focus.md.
//
// Cycle 219 closes the Installed-tab focused-mode gap the cycle-212 report
// deliberately deferred:
//
//   - while the search box has focus and the active tab is Installed, the
//     third /plugin line carries the trailing `(enter to search)` hint
//     inside the same grey-238 span as the rest of the line — measured
//     at 220x50 against p14.out;
//   - typing more characters while focused on Installed does NOT live-
//     filter — the rows below stay unchanged (the cursor keeps its row);
//   - pressing Enter commits the typed query: pluginQueryCommitted
//     flips to true, the rows below filter to the substring matches, and
//     the legend switches from `enter Search` to `enter Expand Details`;
//   - pressing Esc clears the focused state (pluginSearch, pluginQuery and
//     pluginQueryCommitted all reset) and the legend reverts to the
//     cycle-198 default;
//   - the cycle-212 Tab-keeps-focus behaviour is preserved: the
//     commit flag survives a Tab round-trip, so when the user Tabs back
//     to Installed from Discover the focused-state rendering is intact.
//
// The focused Installed state is reached only via Tab from a focused
// Discover box (the cycle-212 probe at 220x50 — see the cycle-212
// deliverable, the screenshot caption "After, focused Discover with query
// 'g', then Tab"). Typing on an unfocused Installed tab does not focus
// the box (cycle 198/204 only seeded the focus on Discover); this
// matches the cycle-212 scope ("Don't change the Tab behavior; just the
// rendering inside the focused state").

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// focusInstalled seeds the focused Installed state by typing on Discover
// and pressing Tab (the cycle-212 path). The query is whatever the test
// types into Discover.
func focusInstalled(t *testing.T, m *runMode, query string) {
	t.Helper()
	m.panel.pluginTab = panelDiscover
	for _, r := range query {
		m.handlePanelKey(tea.Key{Code: r, Text: string(r)})
	}
	if !m.panel.pluginSearch {
		t.Fatalf("setup: typing %q on Discover did not focus the box", query)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelInstalled {
		t.Fatalf("setup: Tab landed on tab=%d, want Installed", m.panel.pluginTab)
	}
	if !m.panel.pluginSearch {
		t.Fatal("setup: Tab left the focused state")
	}
}

// seedInstalled219 seeds the Installed directory tree under root and writes
// a config.json with the supplied names marked enabled. cycle-212's root
// helper builds a fresh tempdir under t.TempDir(); the seed function
// writes against the *same* root so installedPluginRows() sees the
// directories, and seeds config.json so the badges read [enabled].
func seedInstalled219(t *testing.T, root string, names []string, descs []string) {
	t.Helper()
	for i, n := range names {
		seedPlugin204(t, root, n, descs[i], "1.0.0")
	}
	enabled := map[string]bool{}
	for _, n := range names {
		enabled[n] = true
	}
	// write a config.json the cycle-198 helper understands.
	dir := filepath.Join(root, "config")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	plugins := map[string]any{}
	for n, on := range enabled {
		plugins[n] = map[string]any{"enabled": on}
	}
	writeJSONFile(t, filepath.Join(dir, "config.json"), map[string]any{
		"plugins":      plugins,
		"userSettings": map[string]any{"themeMode": "THEME_MODE_INHERIT"},
	})
}

// TestCycle219_InstalledFocusShowsHint pins the search box line on the
// Installed tab while the box has focus and the typed query has not yet
// been committed: `  Search: ` + the query + the (blinking) cursor block +
// the trailing `(enter to search)` hint, all inside the same grey-238
// span. Cycle-212's p14.out captured the line at 220x50; this test pins
// the SGR span verbatim.
func TestCycle219_InstalledFocusShowsHint(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x", "foxtrot-x"},
		[]string{"Alpha body.", "Beta body.",
			"Foxtrot body — checks calendar availability."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "g")

	if m.panel.pluginQuery != "g" {
		t.Fatalf("query = %q, want %q", m.panel.pluginQuery, "g")
	}
	if m.panel.pluginQueryCommitted {
		t.Fatal("query should be un-committed on focus")
	}

	// Blink on: the line carries the cursor block AND the (enter to
	// search) hint. The hint re-opens the grey-238 span after the cursor
	// (matches the reference's p2.out raw SGR).
	m.panel.pluginBlinkOn = true
	want := "\x1b[38;2;88;110;117m  Search: \x1b[39m g\x1b[7m\x1b[37m \x1b[0m\x1b[38;2;88;110;117m  (enter to search)\x1b[39m"
	if got := m.pluginSearchBoxLine(); got != want {
		t.Errorf("focused Installed box (blink on):\n got %q\nwant %q", got, want)
	}

	// Blink off: the cursor span drops to a plain space, but the hint
	// stays (the query is still un-committed).
	m.panel.pluginBlinkOn = false
	wantOff := "\x1b[38;2;88;110;117m  Search: \x1b[39m g \x1b[38;2;88;110;117m  (enter to search)\x1b[39m"
	if got := m.pluginSearchBoxLine(); got != wantOff {
		t.Errorf("focused Installed box (blink off):\n got %q\nwant %q", got, wantOff)
	}
}

// TestCycle219_InstalledFocusLegend pins the Installed tab's footer while
// the box is focused and the typed query has not yet been committed:
//
//	Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Search
//	          space Toggle Enable  ctrl+s Uninstall  esc Exit Search
//
// Once the user presses Enter, the legend's `enter` field switches from
// `Search` to `Expand Details` (cycle-198's default).
func TestCycle219_InstalledFocusLegend(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x"},
		[]string{"Alpha."})

	// Unfocused Installed: the cycle-198 footer (enter Expand Details,
	// esc Exit) plus the cycle-dpl `/ Show Enabled Only` slot.
	m := mkPanel(panelPlugin, "/plugin", 0)
	if got, want := stripSGR(m.pluginInstalledLegend()),
		"Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  space Toggle Enable  ctrl+s Uninstall  esc Exit"; got != want {
		t.Errorf("unfocused legend:\n got %q\nwant %q", got, want)
	}

	// Focused, un-committed: enter -> Search, esc -> Exit Search.
	focusInstalled(t, m, "g")
	if got, want := stripSGR(m.pluginInstalledLegend()),
		"Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Search  space Toggle Enable  ctrl+s Uninstall  esc Exit Search"; got != want {
		t.Errorf("focused un-committed legend:\n got %q\nwant %q", got, want)
	}

	// Pressing Enter commits; the legend switches back to Expand Details.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter on focused Installed did not commit the query")
	}
	if got, want := stripSGR(m.pluginInstalledLegend()),
		"Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  space Toggle Enable  ctrl+s Uninstall  esc Exit Search"; got != want {
		t.Errorf("focused committed legend:\n got %q\nwant %q", got, want)
	}
}

// TestCycle219_TypingDoesNotFilter pins the cycle-219 row-rendering rule:
// typing characters while focused on the Installed tab keeps the rows
// below unfiltered. Before the commit, every installed plugin is rendered
// (alphabetical order), not a filtered subset.
func TestCycle219_TypingDoesNotFilter(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x", "foxtrot-x"},
		[]string{"Alpha body.", "Beta body.",
			"Foxtrot body — checks calendar availability."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "f")

	if !m.panel.pluginSearch || m.panel.pluginQuery != "f" {
		t.Fatalf("setup: search=%v query=%q", m.panel.pluginSearch, m.panel.pluginQuery)
	}

	// Before Enter, all three installed plugins must be visible (the
	// query has not been committed). The cycle-198 row shape carries the
	// badge, name and description in their normal order.
	got := plainLines(m)
	want := []string{
		"  Plugins",
		"  Installed (3)    Discover (1)   (tab to cycle)",
		"  Search:  f   (enter to search)", // cursor dropped to plain space
		"> [enabled]  alpha-x Alpha body.",
		"  [enabled]  beta-x Beta body.",
		"  [enabled]  foxtrot-x Foxtrot body — checks calendar availability.",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Search  space Toggle Enable  ctrl+s Uninstall  esc Exit Search",
	}
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d\n got: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}

	// Typing more characters still does not filter.
	m.handlePanelKey(tea.Key{Code: 'o', Text: "o"})
	m.handlePanelKey(tea.Key{Code: 'x', Text: "x"})
	if got, want := m.panel.pluginQuery, "fox"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	if m.panel.pluginQueryCommitted {
		t.Fatal("typing more characters should keep the query un-committed")
	}
	if got := plainLines(m); len(got) != len(want) {
		t.Fatalf("after more typing: got %d lines, want %d:\n got: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("after more typing line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

// TestCycle219_EnterCommitsAndFilters pins the cycle-219 commit semantics:
// pressing Enter on a focused-but-un-committed Installed box sets
// pluginQueryCommitted=true, swaps the trailing hint from "(enter to
// search)" to "(type to continue searching)" (cycle-219 p3.out,
// measured at 220x50), filters the rows below to the substring matches,
// and switches the legend's `enter` field from `Search` to
// `Expand Details`. After the commit, typing more characters
// live-filters.
func TestCycle219_EnterCommitsAndFilters(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x", "foxtrot-x"},
		[]string{"Alpha body.", "Beta body.",
			"Foxtrot body — checks calendar availability."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "f")

	// Cursor is on row 0 (first installed plugin) before the commit.
	if m.panel.cursor != 0 {
		t.Fatalf("cursor before Enter = %d, want 0", m.panel.cursor)
	}

	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})

	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter did not commit the query")
	}
	if !m.panel.pluginQueryAsleep {
		t.Fatal("Enter should put the cursor to sleep")
	}
	// Box line (committed + asleep): the cursor block is dropped — only
	// the query + a single separator space + the grey-238 hint remain.
	// Verified against the reference: 100 captures over 2.5 s show no
	// cursor-block frame after Enter. Typing more characters re-awakens
	// the cursor (asserted below).
	wantBox := "\x1b[38;2;88;110;117m  Search: \x1b[39mf \x1b[38;2;88;110;117m  (type to continue searching)\x1b[39m"
	if got := m.pluginSearchBoxLine(); got != wantBox {
		t.Errorf("committed box (asleep):\n got %q\nwant %q", got, wantBox)
	}
	// The rows below now filter to the entries matching "f".
	// foxtrot-x starts with "f", so it is the only match.
	got := plainLines(m)
	want := []string{
		"  Plugins",
		"  Installed (1)    Discover (1)   (tab to cycle)",
		"  Search: f   (type to continue searching)",
		"> [enabled]  foxtrot-x Foxtrot body — checks calendar availability.",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Page  tab Cycle Tabs  enter Expand Details  space Toggle Enable  ctrl+s Uninstall  esc Exit Search",
	}
	if len(got) != len(want) {
		t.Fatalf("after Enter: got %d lines, want %d:\n got: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("after Enter line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}

	// After commit, typing more characters live-filters AND the cursor
	// re-awakens (the reference returns the cursor block as soon as
	// the user types — p3-typed.out shows `Search: goo▍  (type...)`).
	m.handlePanelKey(tea.Key{Code: 'o', Text: "o"})
	if m.panel.pluginQueryAsleep {
		t.Fatal("typing should re-awaken the cursor")
	}
	// Force blink-on for a stable string assertion.
	m.panel.pluginBlinkOn = true
	wantAfterType := "\x1b[38;2;88;110;117m  Search: \x1b[39m fo\x1b[7m\x1b[37m \x1b[0m\x1b[38;2;88;110;117m  (type to continue searching)\x1b[39m"
	if got := m.pluginSearchBoxLine(); got != wantAfterType {
		t.Errorf("after typing 'o' (awake, blink on):\n got %q\nwant %q", got, wantAfterType)
	}
	m.handlePanelKey(tea.Key{Code: 'o', Text: "o"})
	m.handlePanelKey(tea.Key{Code: 'x', Text: "x"})
	if m.panel.pluginQuery != "foox" {
		t.Fatalf("query = %q, want %q", m.panel.pluginQuery, "foox")
	}
	if got := plainLines(m); got[2] != "  Search:  foox   (type to continue searching)" {
		t.Errorf("after typing 'foox': box = %q", got[2])
	}
	if !m.panel.pluginQueryCommitted {
		t.Fatal("further typing should keep the query committed")
	}
	if !strings.Contains(plainLines(m)[len(plainLines(m))-1], "enter Expand Details") {
		t.Errorf("legend after further typing:\n%q",
			plainLines(m)[len(plainLines(m))-1])
	}
}

// TestCycle219_NoMatchesScreen pins the empty-filter result on the
// Installed tab: after Enter with a query that matches nothing, the panel
// shows the same "No installed plugins match your search." row the
// Discover tab uses (cycle 204's TestCycle204_NoMatchScreen).
func TestCycle219_NoMatchesScreen(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x"},
		[]string{"Alpha."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "zz")
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})

	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter did not commit")
	}
	got := plainLines(m)
	if got[3] != "  No installed plugins match your search." {
		t.Errorf("no-match line = %q", got[3])
	}
	if got[4] != "" {
		t.Errorf("no-match blank = %q, want empty", got[4])
	}
	if !strings.HasPrefix(got[len(got)-1], "Keyboard: ") {
		t.Errorf("last line = %q, want a legend row", got[len(got)-1])
	}
}

// TestCycle219_EscClearsFocus pins the cycle-219 Esc semantics: pressing
// Esc on a focused Installed box clears pluginSearch, pluginQuery and
// pluginQueryCommitted in one shot, restoring the placeholder and the
// unfocused-tab legend.
func TestCycle219_EscClearsFocus(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x"},
		[]string{"Alpha."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "a")
	if !m.panel.pluginSearch || m.panel.pluginQuery != "a" {
		t.Fatalf("setup: search=%v query=%q",
			m.panel.pluginSearch, m.panel.pluginQuery)
	}
	// Even when committed, Esc must clear every search-related flag.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter did not commit")
	}

	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.pluginSearch {
		t.Fatal("Esc did not clear pluginSearch")
	}
	if m.panel.pluginQuery != "" {
		t.Errorf("pluginQuery = %q, want empty", m.panel.pluginQuery)
	}
	if m.panel.pluginQueryCommitted {
		t.Fatal("Esc did not clear pluginQueryCommitted")
	}
	if m.panel.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.panel.cursor)
	}

	got := plainLines(m)
	if got[2] != "  Type to search plugins..." {
		t.Errorf("box = %q, want placeholder", got[2])
	}
	if !strings.Contains(got[len(got)-1], "enter Expand Details") ||
		!strings.Contains(got[len(got)-1], "esc Exit") ||
		strings.Contains(got[len(got)-1], "Exit Search") {
		t.Errorf("legend should be the cycle-198 unfocused footer:\n%q",
			got[len(got)-1])
	}
}

// TestCycle219_InstalledFilterRule pins the field list the Installed-tab
// filter searches: the plugin name and its description. A query that
// matches only the description still hits; a query that matches neither
// misses.
//
// This mirrors TestCycle204_FilterRule for the Discover tab. The Installed
// description comes from the marketplace (when present) or the
// plugin.json (otherwise) — see pluginDescFor in plugin_data.go — which
// keeps the field set small and identical to the description the row
// already paints.
func TestCycle219_InstalledFilterRule(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{
			{"alpha-x", "Alpha body.", "1.0.0"},
			{"foxtrot-x", "Foxtrot body — checks calendar availability.", "1.0.0"},
		})
	seedInstalled219(t, root,
		[]string{"alpha-x", "foxtrot-x"},
		[]string{"Alpha body.",
			"Foxtrot body — checks calendar availability."})

	rows := installedPluginRows()
	names := func(rs []pluginRow) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.name)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		query string
		want  string
	}{
		{"", "alpha-x,foxtrot-x"},
		{"alpha", "alpha-x"},
		{"ALPHA", "alpha-x"},
		{"fox", "foxtrot-x"},
		{"calendar", "foxtrot-x"},
		{"zzz", ""},
	}
	for _, c := range cases {
		if got := names(pluginInstalledFilter(rows, c.query)); got != c.want {
			t.Errorf("filter %q = %q, want %q", c.query, got, c.want)
		}
	}
}

// TestCycle219_CursorBoundWhileUncommitted pins the cycle-219 ↑/↓ bound:
// while the Installed tab's box has focus but the query has not yet been
// committed, the cursor can move through the full unfiltered list (4
// fallback plugins); once the user presses Enter and types a query that
// matches only one plugin, the bound clamps to 1.
func TestCycle219_CursorBoundWhileUncommitted(t *testing.T) {
	// The fallback list is the four stock plugins; we do not seed any
	// directories so installedPluginRows() returns pluginInstalledFallback.
	cycle212Root(t)
	seedMarketplace204(t, t.TempDir(), "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "g")
	if !m.panel.pluginSearch {
		t.Fatal("setup: not focused")
	}
	if got := m.pluginRowCount(); got != 4 {
		t.Fatalf("un-committed bound = %d, want 4 (full list)", got)
	}
	// Down parks on the last row.
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.cursor != 3 {
		t.Errorf("Down x3 -> cursor=%d, want 3", m.panel.cursor)
	}

	// Commit + narrow the query: nothing matches.
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	m.handlePanelKey(tea.Key{Code: 'z', Text: "z"})
	m.handlePanelKey(tea.Key{Code: 'z', Text: "z"})
	m.handlePanelKey(tea.Key{Code: 'q', Text: "q"})
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter did not commit")
	}
	if got := m.pluginRowCount(); got != 0 {
		t.Fatalf("committed bound = %d, want 0", got)
	}
}

// TestCycle219_TabKeepsCommitFlag pins the cycle-212 Tab-keeps-focus
// behaviour under cycle-219's commit flag: a Tab from a focused
// (un-committed or committed) Installed box cycles to Discover without
// resetting pluginQueryCommitted, so when the user Tabs back to
// Installed the commit flag is preserved (matches the cycle-212 probe).
func TestCycle219_TabKeepsCommitFlag(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x"},
		[]string{"Alpha."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "a")
	if m.panel.pluginQueryCommitted {
		t.Fatal("setup: query should be un-committed on focus")
	}

	// Tab to Discover — flag is preserved (still un-committed).
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelDiscover {
		t.Fatalf("Tab to Discover: tab=%d, want Discover", m.panel.pluginTab)
	}
	if m.panel.pluginQueryCommitted {
		t.Fatal("Tab flipped pluginQueryCommitted to true")
	}

	// Tab back to Installed — flag is preserved (still un-committed).
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelInstalled {
		t.Fatalf("Tab to Installed: tab=%d, want Installed", m.panel.pluginTab)
	}
	if m.panel.pluginQueryCommitted {
		t.Fatal("Tab back to Installed flipped pluginQueryCommitted to true")
	}

	// Commit, then Tab — the commit flag must survive the Tab round-trip
	// so when the user comes back to Installed the filter is live.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter did not commit")
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelDiscover {
		t.Fatalf("after commit+Tab: tab=%d, want Discover", m.panel.pluginTab)
	}
	if !m.panel.pluginQueryCommitted {
		t.Fatal("Tab dropped pluginQueryCommitted while on Discover")
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelInstalled {
		t.Fatalf("back to Installed: tab=%d, want Installed", m.panel.pluginTab)
	}
	if !m.panel.pluginQueryCommitted {
		t.Fatal("Tab back to Installed dropped pluginQueryCommitted")
	}
}

// TestCycle219_CursorStaysWhileTyping pins the cursor behaviour while the
// user is composing the query on a focused but un-committed Installed
// box: the rows are unfiltered, so the cursor stays where the user left
// it. (On Discover the cursor re-parks on the first filtered result;
// cycle-219 keeps Discover's existing behaviour.)
func TestCycle219_CursorStaysWhileTyping(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x", "foxtrot-x"},
		[]string{"Alpha body.", "Beta body.",
			"Foxtrot body — checks calendar availability."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "a")
	// Park on row 1.
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.cursor != 1 {
		t.Fatalf("setup: cursor=%d, want 1", m.panel.cursor)
	}
	// Typing more characters does not move the cursor on Installed.
	m.handlePanelKey(tea.Key{Code: 'l', Text: "l"})
	m.handlePanelKey(tea.Key{Code: 'p', Text: "p"})
	m.handlePanelKey(tea.Key{Code: 'h', Text: "h"})
	m.handlePanelKey(tea.Key{Code: 'a', Text: "a"})
	if m.panel.cursor != 1 {
		t.Errorf("typing left the cursor at %d, want 1", m.panel.cursor)
	}
	if m.panel.pluginQuery != "alpha" {
		t.Errorf("query = %q, want %q", m.panel.pluginQuery, "alpha")
	}

	// Commit. The Installed list filters down to alpha-x only;
	// alpha-x sits at index 0 of the filtered list. pluginCursorFor
	// re-clamps the cursor to fit (cursor=1 -> cursor=0) when the next
	// frame renders — verify by inspecting the rendered first row.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter did not commit")
	}
	if got := plainLines(m)[3]; !strings.HasPrefix(got, "> [enabled]  alpha-x ") {
		t.Errorf("after commit: first row = %q, want alpha-x as cursor", got)
	}
}

// TestCycle219_FocusedEnterDoesNotExpand pins the un-committed-mode
// rule: Enter on a focused-but-un-committed Installed box commits but
// does NOT open the detail block. (Enter on a focused-AND-committed box
// opens the detail block, like the cycle-198 unfocused tab.)
func TestCycle219_FocusedEnterDoesNotExpand(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x"},
		[]string{"Alpha."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "a")
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})

	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter did not commit")
	}
	if m.panel.pluginExpanded["alpha-x"] {
		t.Fatal("Enter on un-committed Installed should not expand the detail block")
	}

	// A second Enter expands the detail block.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginExpanded["alpha-x"] {
		t.Fatal("second Enter on committed Installed did not expand the detail block")
	}
}

// TestCycle219_InstalledDisabledWhileFocusOpen pins the local-install
// prompt / search-box mutual exclusion (cycle 212) under cycle 219: the
// prompt is opened by Enter on the local-install row of the unfocused
// Discover tab. The prompt and the focused box are mutually exclusive,
// so opening the prompt must not also focus the search box.
func TestCycle219_InstalledDisabledWhileFocusOpen(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.openPluginLocalInstall()
	// pluginSearch is still false — the prompt owns the keys.
	if m.panel.pluginSearch {
		t.Fatal("opening the local-install prompt should not also focus the search box")
	}
}
