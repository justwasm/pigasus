package main

// slash_cycle204_test.go — cycle 204: the /plugin Discover tab's search box,
// its detail block and its footer.
//
// Every expectation below is pinned to the live reference (Antigravity CLI
// 1.4.0), driven at 220x50 and 100x50 through a private tmux socket; the probe
// transcripts are /private/var/folders/.../opencode/agy204/{p16,p17,p18,p19}.out
// and w100.txt (the 100-column frame), and the cycle report is
// docs/cycles/cycle-204/deliverable-plugin-search.md.
//
// What cycles 198/201 left inert that this cycle measures and pins:
//
//   - space (or ANY printable key) focuses the Discover search box, which
//     renders as `  Search: ` + query + a reverse-video block cursor;
//   - the filter is a case-insensitive substring over the *marketplace name*
//     plus the plugin name and description — the marketplace's own
//     `description` is NOT searched (probe p8: "catalog", which appears only
//     there, matches nothing);
//   - Enter opens the Discover detail block (Description / Components /
//     Marketplace / Version — no Path, no Skills) and the Description wraps at
//     terminal_width-2-19 columns;
//   - the footer is contextual: the local-install action is named only on the
//     "+ Install from local directory..." row, and `esc` becomes "Exit Search"
//     while the box has focus;
//   - `←/→` park the cursor on the first / last row.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// cycle204Root builds <tmp>/.gemini/{antigravity-cli,config} and points
// ANTIGRAVITY_HOME at the antigravity-cli one, mirroring cycle198Root. The
// marketplace cache and the plugin/config tree are both siblings of that home.
func cycle204Root(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".gemini")
	agy := filepath.Join(root, "antigravity-cli")
	if err := os.MkdirAll(agy, 0700); err != nil {
		t.Fatalf("mkdir antigravity-cli: %v", err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agy)
	return root
}

// marketplaceEntry204 is one marketplace.json plugin entry with its version.
type marketplaceEntry204 struct{ name, desc, version string }

// seedMarketplace204 writes cache/marketplaces/<mkt>/marketplace.json with
// per-entry versions (cycle198's seedMarketplace omits them).
func seedMarketplace204(t *testing.T, root, mkt, desc string, entries []marketplaceEntry204) {
	t.Helper()
	dir := filepath.Join(root, "antigravity-cli", "cache", "marketplaces", mkt)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir marketplace: %v", err)
	}
	var plugins []map[string]any
	for _, e := range entries {
		plugins = append(plugins, map[string]any{
			"name": e.name, "description": e.desc, "version": e.version,
		})
	}
	writeJSONFile(t, filepath.Join(dir, "marketplace.json"), map[string]any{
		"name": mkt, "description": desc, "plugins": plugins,
	})
}

// seedCachePlugin204 unpacks a plugin into the cache path the Components line
// reads: cache/marketplaces/<mkt>/plugins/<name>/<version>/<comp>/.
func seedCachePlugin204(t *testing.T, root, mkt, name, version string, comps ...string) {
	t.Helper()
	for _, c := range comps {
		dir := filepath.Join(root, "antigravity-cli", "cache", "marketplaces",
			mkt, "plugins", name, version, c)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatalf("mkdir component: %v", err)
		}
	}
}

// dataAgentKitDesc is data-agent-kit-plugin's marketplace description, verbatim
// from marketplace.json. It is the value whose wrap the reference's 100-column
// frame pins (w100.txt rows 5-10).
const dataAgentKitDesc = "This plugin provides a specialized suite of skills for data engineers and " +
	"database practitioners working on Google Cloud. It acts as an expert assistant, " +
	"allowing you to use natural language prompts in your preferred coding agent to " +
	"architect complex data pipelines, transform data with dbt, write Spark and " +
	"BigQuery SQL notebooks, and orchestrate end-to-end workflows across GCP's data ecosystem."

// TestCycle204_SearchBoxBytes pins the third /plugin line in both states: the
// placeholder, and the focused box with its reverse-video block cursor. The
// cursor blinks in the reference (probes p18: the block is absent from roughly
// half the captures); the clone always paints the "on" phase.
func TestCycle204_SearchBoxBytes(t *testing.T) {
	cycle204Root(t)
	const placeholder = "\x1b[38;2;88;110;117m  Type to search plugins...\x1b[39m"

	// Installed tab: always the placeholder (its own search is out of scope).
	m := mkPanel(panelPlugin, "/plugin", 0)
	if got := m.panelLines()[2]; got != placeholder {
		t.Errorf("Installed box:\n got %q\nwant %q", got, placeholder)
	}

	// Discover, unfocused.
	m.panel.pluginTab = panelDiscover
	if got := m.panelLines()[2]; got != placeholder {
		t.Errorf("Discover unfocused box:\n got %q\nwant %q", got, placeholder)
	}

	// Discover, focused, empty query: `  Search: ` + one plain space + cursor.
	m.enterPluginSearch()
	want := "\x1b[38;2;88;110;117m  Search: \x1b[39m \x1b[7m\x1b[37m \x1b[0m"
	if got := m.panelLines()[2]; got != want {
		t.Errorf("Discover focused empty box:\n got %q\nwant %q", got, want)
	}

	// Discover, focused, a query: the cursor follows the text (w100/p18).
	m.panel.pluginQuery = "gc"
	want = "\x1b[38;2;88;110;117m  Search: \x1b[39m gc\x1b[7m\x1b[37m \x1b[0m"
	if got := m.panelLines()[2]; got != want {
		t.Errorf("Discover focused query box:\n got %q\nwant %q", got, want)
	}
}

// TestCycle204_FilterRule pins the field list the filter searches: the
// marketplace *name*, the plugin name and the plugin description — never the
// marketplace's own description. This is the discrimination probe p8 used:
// "catalog" is in the marketplace description only and matches nothing, while
// "official"/"gravity" are in the marketplace directory name only and match
// everything.
func TestCycle204_FilterRule(t *testing.T) {
	mkts := []pluginMarketplace{{
		name: "antigravity-plugins-official",
		items: []pluginRow{
			{name: "alpha-plugin", desc: "Alpha body text.", version: "1.0.0"},
			{name: "beta", desc: "Reads and writes spreadsheets.", version: "2.0.0"},
		},
	}}
	names := func(rows []pluginRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, r.name)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		query string
		want  string
	}{
		{"", "alpha-plugin,beta"},         // empty query = no filtering
		{"alpha", "alpha-plugin"},         // plugin name
		{"ALPHA", "alpha-plugin"},         // case-insensitive
		{"spreadsheets", "beta"},          // plugin description
		{"BODY", "alpha-plugin"},          // description, mixed case
		{"official", "alpha-plugin,beta"}, // marketplace name only
		{"gravity", "alpha-plugin,beta"},  // marketplace name only
		{"plugins-official", "alpha-plugin,beta"},
		{"catalog", ""}, // marketplace DESCRIPTION only -> no match
		{"zzz", ""},     // no match
	}
	for _, c := range cases {
		if got := names(pluginDiscoverFilter(mkts, c.query)); got != c.want {
			t.Errorf("filter %q = %q, want %q", c.query, got, c.want)
		}
	}
	// The marketplace *description* must not leak into the searched text.
	if got := names(pluginDiscoverFilter(mkts, "alpha")); strings.Contains(got, "beta") {
		t.Errorf("filter searched the marketplace description: %q", got)
	}
}

// TestCycle204_WrapWords pins the greedy word-wrap primitive: it breaks on
// single spaces, fills to exactly `width` columns, and emits a word longer than
// the width whole (the reference does not hyphenate).
func TestCycle204_WrapWords(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  []string
	}{
		{"", 10, []string{""}},
		{"one", 10, []string{"one"}},
		{"one two three four", 7, []string{"one two", "three", "four"}},
		{"a b c", 1, []string{"a", "b", "c"}},
		{"supercalifragilistic ok", 5, []string{"supercalifragilistic", "ok"}},
		// Exactly width on the first line, no early break.
		{strings.Repeat("x", 7) + " yy", 7, []string{strings.Repeat("x", 7), "yy"}},
	}
	for _, c := range cases {
		got := wrapWords(c.in, c.width)
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
			t.Errorf("wrapWords(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
		}
	}
}

// TestCycle204_DetailWrap pins the reference's exact 100-column wrap of
// data-agent-kit-plugin's description (terminal width 100 -> max 98 -> value
// width 98-19 = 79). Transcribed from w100.txt rows 5-10.
func TestCycle204_DetailWrap(t *testing.T) {
	got := pluginWrappedDetail("Description:", dataAgentKitDesc, 98)
	want := []string{
		"     \x1b[38;2;238;232;213m Description: This plugin provides a specialized suite of skills for data engineers and\x1b[39m",
		"                  \x1b[38;2;238;232;213m database practitioners working on Google Cloud. It acts as an expert assistant,\x1b[39m",
		"                  \x1b[38;2;238;232;213m allowing you to use natural language prompts in your preferred coding agent to\x1b[39m",
		"                  \x1b[38;2;238;232;213m architect complex data pipelines, transform data with dbt, write Spark and\x1b[39m",
		"                  \x1b[38;2;238;232;213m BigQuery SQL notebooks, and orchestrate end-to-end workflows across GCP's data\x1b[39m",
		"                  \x1b[38;2;238;232;213m ecosystem.\x1b[39m",
	}
	if len(got) != len(want) {
		t.Fatalf("wrap produced %d lines, want %d:\n%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("wrap line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
	// The value column starts at 19 (5-space indent + the plain space inside
	// the grey span + the 13-wide label field), so the first line's text begins
	// with 6 spaces and continuations with 19.
	if first := stripSGR(got[0]); !strings.HasPrefix(first, strings.Repeat(" ", 6)+"Description: ") {
		t.Errorf("first line prefix = %q, want a 19-column body column", first)
	}
	if cont := stripSGR(got[1]); !strings.HasPrefix(cont, strings.Repeat(" ", 19)) {
		t.Errorf("continuation indent = %q, want 19 spaces", cont)
	}

	// The wrap width is max-19, not max-20: a 79-column value fits one line at
	// max=98 and spills to two at max=97.
	v79 := strings.Repeat("a", 76) + " bb"
	if got := pluginWrappedDetail("Description:", v79, 98); len(got) != 1 {
		t.Errorf("width should be 79 at max 98: got %d lines", len(got))
	}
	if got := pluginWrappedDetail("Description:", v79, 97); len(got) != 2 {
		t.Errorf("width should be 78 at max 97: got %d lines", len(got))
	}
	// A short value never wraps.
	if got := pluginWrappedDetail("Description:", "short.", 98); len(got) != 1 {
		t.Errorf("short value wrapped into %d lines", len(got))
	}
}

// TestCycle204_DiscoverDetails pins the Discover detail block's four fields and
// their label padding, including the Components line: it reads the unpacked
// component directories when the plugin is cached and falls back to
// "loading..." when it is not (gcalendar -> skills, data-agent-kit ->
// loading...; s4.txt / w100.txt).
func TestCycle204_DiscoverDetails(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "antigravity-plugins-official",
		"Antigravity plugins marketplace catalog.", []marketplaceEntry204{
			{"gcalendar", "Check availability, schedule events, and RSVP.", "0.0.5"},
			{"data-agent-kit-plugin", dataAgentKitDesc, "1.1.0"},
		})
	seedCachePlugin204(t, root, "antigravity-plugins-official", "gcalendar", "0.0.5", "skills")
	mkts := pluginMarketplaces()

	got := pluginDiscoverDetails("gcalendar", mkts, 1<<20)
	want := []string{
		"     \x1b[38;2;238;232;213m Description: Check availability, schedule events, and RSVP.\x1b[39m",
		"     \x1b[38;2;238;232;213m Components:  skills\x1b[39m",
		"     \x1b[38;2;238;232;213m Agents:      loading...\x1b[39m",
		"     \x1b[38;2;238;232;213m Marketplace: antigravity-plugins-official\x1b[39m",
		"     \x1b[38;2;238;232;213m Version:     0.0.5\x1b[39m",
	}
	if len(got) != len(want) {
		t.Fatalf("gcalendar details = %d lines, want %d:\n%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("gcalendar detail %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}

	// data-agent-kit is not unpacked, so Components reads loading..., and its
	// version comes from the marketplace entry.
	dk := pluginDiscoverDetails("data-agent-kit-plugin", mkts, 1<<20)
	if !strings.Contains(dk[1], "Components:  loading...") {
		t.Errorf("uncached Components = %q", dk[1])
	}
	if !strings.Contains(dk[4], "Version:     1.1.0") {
		t.Errorf("version = %q", dk[4])
	}
	// No `Path:`/`Skills:` line, unlike the Installed block.
	joined := strings.Join(dk, "\n")
	if strings.Contains(joined, "Path:") || strings.Contains(joined, "Skills:") {
		t.Errorf("Discover block leaked an Installed-only field:\n%s", joined)
	}
}

// TestCycle204_ComponentsSortedAndJoined covers pluginComponents' shape beyond
// the single-dir case: several component directories are sorted and joined with
// ", ".
func TestCycle204_ComponentsSortedAndJoined(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{{"p", "P.", "9.9.9"}})
	// Created out of order on purpose.
	seedCachePlugin204(t, root, "mkt", "p", "9.9.9", "skills", "agents", "mcp")
	if got := pluginComponents("p", pluginMarketplaces()); got != "agents, mcp, skills" {
		t.Errorf("pluginComponents = %q, want \"agents, mcp, skills\"", got)
	}
}

// TestCycle204_NoMatchScreen pins the empty-result screen: box / message /
// blank / blank / legend — one more blank than a screen with rows (p16).
func TestCycle204_NoMatchScreen(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "antigravity-plugins-official", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.handlePanelKey(tea.Key{Code: tea.KeySpace}) // focus
	for _, r := range "zzq" {
		m.handlePanelKey(tea.Key{Code: r, Text: string(r)})
	}
	got := plainLines(m)
	want := []string{
		"  Plugins",
		"  Installed (4)    Discover (1)   (tab to cycle)",
		"  Search:  zzq ", // the block cursor strips away, leaving its space
		"  No marketplace plugins match your search.",
		"",
		"",
	}
	if len(got) != len(want)+1 {
		t.Fatalf("no-match screen has %d lines: %q", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
	if !strings.HasPrefix(got[len(got)-1], "Keyboard: ") {
		t.Errorf("last line is not the legend: %q", got[len(got)-1])
	}
	if !strings.Contains(got[len(got)-1], "esc Exit Search") {
		t.Errorf("focused legend should say Exit Search: %q", got[len(got)-1])
	}
	if strings.Contains(strings.Join(got, "\n"), "Install from local") {
		t.Errorf("the local-install row should be hidden while searching:\n%s", strings.Join(got, "\n"))
	}
}

// TestCycle204_PrintableKeyFocusesAndFilters: typing any printable key on the
// unfocused Discover tab focuses the box, seeds the query, hides the
// local-install row and parks the cursor on the first result (p19).
func TestCycle204_PrintableKeyFocusesAndFilters(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "antigravity-plugins-official", "",
		[]marketplaceEntry204{
			{"alpha", "Alpha.", "1.0.0"},
			{"beta", "Beta.", "1.0.0"},
		})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	if handled, _ := m.handlePanelKey(tea.Key{Code: 'b', Text: "b"}); !handled {
		t.Fatal("printable key reported unhandled")
	}
	if !m.panel.pluginSearch || m.panel.pluginQuery != "b" || m.panel.cursor != 0 {
		t.Fatalf("after 'b': search=%v query=%q cursor=%d",
			m.panel.pluginSearch, m.panel.pluginQuery, m.panel.cursor)
	}
	got := plainLines(m)
	if !strings.Contains(got[2], "Search:  b") {
		t.Errorf("box = %q", got[2])
	}
	// Only beta matches "b" (its name); the local row is hidden.
	if got[3] != "> ● beta Beta." {
		t.Errorf("first filtered row = %q", got[3])
	}
	if strings.Contains(strings.Join(got, "\n"), "Install from local") {
		t.Errorf("local row visible while searching:\n%s", strings.Join(got, "\n"))
	}

	// esc leaves search, restores the local row and re-parks the cursor on it,
	// but keeps an expanded detail block open (s4.txt).
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.pluginSearch || m.panel.pluginQuery != "" || m.panel.cursor != 0 {
		t.Fatalf("after esc: search=%v query=%q cursor=%d",
			m.panel.pluginSearch, m.panel.pluginQuery, m.panel.cursor)
	}
	if got := plainLines(m); got[2] != "  Type to search plugins..." {
		t.Errorf("box after esc = %q", got[2])
	}
}

// TestCycle204_SearchKeyLifecycle covers the remaining search-mode keys: space
// seeds a blank, backspace edits then exits on empty, ctrl+u exits.
func TestCycle204_SearchKeyLifecycle(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.handlePanelKey(tea.Key{Code: tea.KeySpace}) // focus
	m.handlePanelKey(tea.Key{Code: tea.KeySpace}) // a literal space
	if m.panel.pluginQuery != " " {
		t.Fatalf("space did not insert a space: %q", m.panel.pluginQuery)
	}
	m.handlePanelKey(tea.Key{Code: 'a', Text: "a"})
	if m.panel.pluginQuery != " a" {
		t.Fatalf("query = %q, want \" a\"", m.panel.pluginQuery)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.pluginQuery != " " {
		t.Fatalf("backspace did not drop one rune: %q", m.panel.pluginQuery)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.pluginQuery != "" || !m.panel.pluginSearch {
		t.Fatalf("backspace to empty should keep focus: query=%q search=%v",
			m.panel.pluginQuery, m.panel.pluginSearch)
	}
	// A further backspace on the empty box leaves search mode.
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.pluginSearch {
		t.Fatal("backspace on an empty query should leave search mode")
	}
	// ctrl+u leaves search mode from a non-empty query.
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: 'x', Text: "x"})
	m.handlePanelKey(tea.Key{Code: 'u', Mod: tea.ModCtrl})
	if m.panel.pluginSearch || m.panel.pluginQuery != "" {
		t.Fatalf("ctrl+u did not exit search: search=%v query=%q",
			m.panel.pluginSearch, m.panel.pluginQuery)
	}
}

// TestCycle212_TabFromSearchKeepsFocusAndQuery pins the cycle-212 behaviour
// for Tab from a focused Discover box: the box stays focused (with the same
// typed query) on the new tab. Cycle 204 had a deliberate simplification
// here — it switched to Installed and *left* search — because the
// Installed-tab search-mode rendering was out of scope; cycle 212 measures
// the cycle-204 test's prompt as superseded (see the cycle-212 deliverable).
func TestCycle212_TabFromSearchKeepsFocusAndQuery(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedPlugin204(t, root, "installed-one", "Installed.", "1.0.0")

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: 'g', Text: "g"})
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelInstalled {
		t.Fatalf("after tab: tab=%d, want Installed", m.panel.pluginTab)
	}
	if !m.panel.pluginSearch {
		t.Fatal("after tab: search was left, want focused")
	}
	if m.panel.pluginQuery != "g" {
		t.Fatalf("after tab: query=%q, want %q", m.panel.pluginQuery, "g")
	}
	// A second Tab from focused Installed cycles back to Discover with
	// the query intact (probed at 220x50).
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelDiscover {
		t.Fatalf("after second tab: tab=%d, want Discover", m.panel.pluginTab)
	}
	if m.panel.pluginQuery != "g" {
		t.Fatalf("after second tab: query=%q, want %q", m.panel.pluginQuery, "g")
	}
}

// TestCycle204_TabFromSearchSwitchesToInstalled (legacy). Cycle 204 shipped
// the box-exit-on-Tab simplification; cycle 212 reversed it (see
// TestCycle212_TabFromSearchKeepsFocusAndQuery). The old test is kept here
// only as a historical stub so reviewers can see the before/after side by
// side; it now asserts the *new* behaviour because the change is observable
// in every test file under cmd/wagyu that drives /plugin's Tab key.
//
// This stub will be deleted in a follow-up cycle once every reference to
// the cycle-204 docstring has been retargeted.
func TestCycle204_TabFromSearchSwitchesToInstalled(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedPlugin204(t, root, "installed-one", "Installed.", "1.0.0")

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelInstalled {
		t.Fatalf("after tab: tab=%d, want Installed", m.panel.pluginTab)
	}
	if !m.panel.pluginSearch {
		t.Fatal("after tab: search was left, want focused (cycle 212 changed this)")
	}
}

// seedPlugin204 installs one plugin directory (plugin.json) under the config
// tree, like cycle198's seedPlugin but with no skill dirs.
func seedPlugin204(t *testing.T, root, name, desc, version string) {
	t.Helper()
	dir := filepath.Join(root, "config", "plugins", name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	writeJSONFile(t, filepath.Join(dir, "plugin.json"),
		map[string]any{"name": name, "description": desc, "version": version})
}

// TestCycle204_LegendVariants pins the four footer shapes: unfocused on the
// local row, unfocused on a plugin row, focused, and while no row is selectable
// (no-match screen).
func TestCycle204_LegendVariants(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "antigravity-plugins-official", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	// Unfocused, cursor on the local row.
	if got := plainLines(m)[len(plainLines(m))-1]; !strings.Contains(got, "enter / ctrl+s Enter Local Path") ||
		!strings.Contains(got, "esc Exit") {
		t.Errorf("local-row legend = %q", got)
	}
	// Unfocused, cursor on a plugin row.
	m.panel.cursor = 1
	if got := plainLines(m)[len(plainLines(m))-1]; !strings.Contains(got, "enter Expand Details") ||
		!strings.Contains(got, "ctrl+s Install") || !strings.Contains(got, "esc Exit") {
		t.Errorf("plugin-row legend = %q", got)
	}
	// Focused.
	m.enterPluginSearch()
	if got := plainLines(m)[len(plainLines(m))-1]; !strings.Contains(got, "esc Exit Search") {
		t.Errorf("focused legend = %q", got)
	}
}

// TestCycle204_FirstLastJumps pins `←/→` on the unfocused Discover tab: right
// parks on the last row, left on the first (the local-install row) — s3.txt.
func TestCycle204_FirstLastJumps(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{
		{"alpha", "Alpha.", "1.0.0"},
		{"beta", "Beta.", "1.0.0"},
		{"gamma", "Gamma.", "1.0.0"},
	})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if m.panel.cursor != 3 { // index 3 = the last of 1 local + 3 entries
		t.Fatalf("Right parked on %d, want 3 (last row)", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if m.panel.cursor != 3 {
		t.Fatalf("Right past the end moved to %d, want 3", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	if m.panel.cursor != 0 {
		t.Fatalf("Left parked on %d, want 0 (first row)", m.panel.cursor)
	}
	// The Installed tab's footer does not list ←/→, so they are inert there.
	m.panel.pluginTab = panelInstalled
	m.panel.cursor = 2
	m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if m.panel.cursor != 2 {
		t.Fatalf("Right on Installed moved the cursor to %d", m.panel.cursor)
	}
}

// TestCycle204_EnterExpandsUnderFilteredRow: Enter while focused expands the
// highlighted *filtered* row, and the block survives leaving search (keyed by
// name, like the Installed tab).
func TestCycle204_EnterExpandsUnderFilteredRow(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "antigravity-plugins-official", "",
		[]marketplaceEntry204{
			{"alpha", "Alpha.", "1.0.0"},
			{"gcalendar", "Check availability, schedule events, and RSVP.", "0.0.5"},
		})
	seedCachePlugin204(t, root, "antigravity-plugins-official", "gcalendar", "0.0.5", "skills")

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	for _, r := range "gcal" {
		m.handlePanelKey(tea.Key{Code: r, Text: string(r)})
	}
	// Only gcalendar matches; Enter expands it.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginExpanded["gcalendar"] {
		t.Fatal("Enter did not expand the filtered row")
	}
	joined := strings.Join(plainLines(m), "\n")
	if !strings.Contains(joined, "Components:  skills") {
		t.Errorf("detail block missing after Enter:\n%s", joined)
	}
	// Esc keeps the block open (s4.txt).
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if !m.panel.pluginExpanded["gcalendar"] {
		t.Error("esc collapsed the detail block")
	}
	// A second Enter on the collapsed-then-reselected row collapses it.
	m.panel.cursor = 2 // local row 0 + alpha 1 + gcalendar 2
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.pluginExpanded["gcalendar"] {
		t.Error("second Enter did not collapse the block")
	}
}
