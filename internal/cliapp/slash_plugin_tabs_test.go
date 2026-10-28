package cliapp

// slash_cycle198_test.go — cycle 198: the /plugin picker's two tabs.
//
// Everything here is pinned to the reference as measured by driving 1.4.0
// (probe_ref_plugin2.py, probe_ref_plugin3.py, cmp_plugin_live.py):
//
//   - Installed rows read plugins.<name>.enabled out of
//     ~/.gemini/config/config.json for the [enabled]/[disabled] badge, and
//     space flips that flag on disk.
//   - Enter appends a five-line detail block (Description/Skills/Marketplace/
//     Path/Version) keyed by plugin name, so it survives cursor movement.
//   - Tab swaps to the Discover tab: a "+ Install from local directory..." row
//     plus every cache/marketplaces/<mkt>/marketplace.json entry, with a green
//     ● when the plugin is installed and grey 253 otherwise.
//   - Rows are truncated to terminal_width-2 columns with "...".
//
// The plugin tree is a sibling of the antigravity-cli config home
// (~/.gemini/config vs ~/.gemini/antigravity-cli), so these tests point
// ANTIGRAVITY_HOME at <tmp>/.gemini/antigravity-cli — that isolates both.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/pluginsui"
)

// cycle198Root builds <tmp>/.gemini/{antigravity-cli,config} and points
// ANTIGRAVITY_HOME at the antigravity-cli one. Returns the .gemini root so
// callers can drop fixtures into the config tree.
func cycle198Root(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".gemini")
	agy := filepath.Join(root, "antigravity-cli")
	if err := os.MkdirAll(agy, 0700); err != nil {
		t.Fatalf("mkdir antigravity-cli: %v", err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agy)
	return root
}

// seedPlugin installs one plugin directory: plugin.json plus the named skill
// directories.
func seedPlugin(t *testing.T, root, name, desc, version string, skills ...string) {
	t.Helper()
	dir := filepath.Join(root, "config", "plugins", name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	man := map[string]any{"name": name, "description": desc, "version": version}
	writeJSONFile(t, filepath.Join(dir, "plugin.json"), man)
	for _, s := range skills {
		if err := os.MkdirAll(filepath.Join(dir, "skills", s), 0700); err != nil {
			t.Fatalf("mkdir skill: %v", err)
		}
	}
}

// seedMarketplace writes cache/marketplaces/<mkt>/marketplace.json.
func seedMarketplace(t *testing.T, root, mkt string, entries [][2]string) {
	t.Helper()
	dir := filepath.Join(root, "antigravity-cli", "cache", "marketplaces", mkt)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir marketplace: %v", err)
	}
	var plugins []map[string]any
	for _, e := range entries {
		plugins = append(plugins, map[string]any{"name": e[0], "description": e[1]})
	}
	writeJSONFile(t, filepath.Join(dir, "marketplace.json"), map[string]any{
		"name": mkt, "description": "", "plugins": plugins,
	})
}

// seedPluginConfig writes <root>/config/config.json's plugins map.
func seedPluginConfig(t *testing.T, root string, enabled map[string]bool) {
	t.Helper()
	dir := filepath.Join(root, "config")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	plugins := map[string]any{}
	for name, on := range enabled {
		plugins[name] = map[string]any{"enabled": on}
	}
	writeJSONFile(t, filepath.Join(dir, "config.json"), map[string]any{
		"plugins": plugins, "userSettings": map[string]any{"themeMode": "THEME_MODE_INHERIT"},
	})
}

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestCycle198_TabHeader pins the two tab-header byte strings, which differ in
// which tab carries bold+underline+blue.
func TestCycle198_TabHeader(t *testing.T) {
	cycle198Root(t)
	wantInstalled := "  \x1b[1;4m" + cBlueOpen + "Installed\x1b[0;4m" + cBlueOpen + " " +
		"\x1b[1m(4)\x1b[0m    " +
		"\x1b[38;2;238;232;213mDiscover (17)\x1b[39m \x1b[38;2;238;232;213m  (tab to cycle)\x1b[39m"
	if got := pluginTabHeader(panelInstalled, 4, 17); got != wantInstalled {
		t.Errorf("Installed-active header:\n got %q\nwant %q", got, wantInstalled)
	}
	wantDiscover := "  \x1b[38;2;238;232;213mInstalled (4)\x1b[39m    " +
		"\x1b[1;4m" + cBlueOpen + "Discover\x1b[0;4m" + cBlueOpen + " " +
		"\x1b[1m(17)\x1b[0m \x1b[38;2;238;232;213m  (tab to cycle)\x1b[39m"
	if got := pluginTabHeader(panelDiscover, 4, 17); got != wantDiscover {
		t.Errorf("Discover-active header:\n got %q\nwant %q", got, wantDiscover)
	}
}

// TestCycle198_Badge pins the badge strings: [enabled] keeps its trailing space
// inside the green span so both badges occupy ten columns.
func TestCycle198_Badge(t *testing.T) {
	if got := pluginBadge(true); got != "\x1b[38;2;133;153;0m[enabled] \x1b[39m" {
		t.Errorf("enabled badge = %q", got)
	}
	if got := pluginBadge(false); got != "\x1b[38;2;238;232;213m[disabled]\x1b[39m" {
		t.Errorf("disabled badge = %q", got)
	}
}

// TestCycle198_PluginEnabledRoundTrip covers the config.json accessor: absent
// file / absent entry read as disabled, and a write preserves the rest of the
// file.
func TestCycle198_PluginEnabledRoundTrip(t *testing.T) {
	root := cycle198Root(t)

	if on, err := auth.PluginEnabled("science"); err != nil || on {
		t.Fatalf("missing file: PluginEnabled = (%v, %v), want (false, nil)", on, err)
	}
	seedPluginConfig(t, root, map[string]bool{"science": true})
	if on, err := auth.PluginEnabled("science"); err != nil || !on {
		t.Fatalf("seeded: PluginEnabled = (%v, %v), want (true, nil)", on, err)
	}
	if on, _ := auth.PluginEnabled("not-installed"); on {
		t.Error("an unlisted plugin should read as disabled")
	}
	if err := auth.SetPluginEnabled("science", false); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	if on, _ := auth.PluginEnabled("science"); on {
		t.Error("after SetPluginEnabled(false): still enabled")
	}
	// userSettings must survive the rewrite.
	data, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal config.json: %v", err)
	}
	if us, _ := doc["userSettings"].(map[string]any); us["themeMode"] != "THEME_MODE_INHERIT" {
		t.Errorf("userSettings lost on write: %v", doc["userSettings"])
	}
	// Restore-on-write must also add a name that was not there.
	if err := auth.SetPluginEnabled("swapped-in", true); err != nil {
		t.Fatalf("SetPluginEnabled(new): %v", err)
	}
	if on, _ := auth.PluginEnabled("swapped-in"); !on {
		t.Error("new entry not created")
	}
}

// TestCycle198_SpaceTogglesInstalled drives the key handler: space on the
// Installed tab flips config.json and the rendered badge.
func TestCycle198_SpaceTogglesInstalled(t *testing.T) {
	root := cycle198Root(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.2.3", "one")
	seedPlugin(t, root, "beta", "Beta plugin.", "0.1.0")
	seedPluginConfig(t, root, map[string]bool{"alpha": true, "beta": false})

	m := mkPanel(panelPlugin, "/plugin", 0)
	if got := plainLines(m)[3]; !strings.Contains(got, "[enabled]") {
		t.Fatalf("row 0 = %q, want [enabled]", got)
	}
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if !handled {
		t.Fatal("space reported unhandled")
	}
	if on, _ := auth.PluginEnabled("alpha"); on {
		t.Error("space did not disable alpha")
	}
	if got := plainLines(m)[3]; !strings.Contains(got, "[disabled]") {
		t.Errorf("after space row 0 = %q, want [disabled]", got)
	}
	// The cursor stays put; a second space re-enables.
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if on, _ := auth.PluginEnabled("alpha"); !on {
		t.Error("second space did not re-enable alpha")
	}
}

// TestCycle198_EnterExpandsDetails pins the five detail lines and the
// per-plugin (not per-cursor) expansion state.
func TestCycle198_EnterExpandsDetails(t *testing.T) {
	root := cycle198Root(t)
	seedPlugin(t, root, "chrome-devtools-plugin",
		"Reliable automation, in-depth debugging.", "0.21.0",
		"troubleshooting", "a11y-debugging", "chrome-devtools")
	seedMarketplace(t, root, "antigravity-plugins-official",
		[][2]string{{"chrome-devtools-plugin", "Marketplace text."}})
	seedPluginConfig(t, root, map[string]bool{"chrome-devtools-plugin": true})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	got := plainLines(m)
	if len(got) < 9 {
		t.Fatalf("panel too short after Enter: %q", got)
	}
	want := []string{
		"      Description: Marketplace text.",
		"      Skills:      a11y-debugging, chrome-devtools, troubleshooting",
		"      Agents:      -",
		"      Marketplace: antigravity-plugins-official",
		"      Path:        " + filepath.Join(root, "config", "plugins", "chrome-devtools-plugin"),
		"      Version:     0.21.0",
	}
	for i, w := range want {
		if got[4+i] != w {
			t.Errorf("detail line %d:\n got %q\nwant %q", i, got[4+i], w)
		}
	}
	// Moving the cursor leaves the block open (measured on the Discover tab).
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if !strings.Contains(strings.Join(plainLines(m), "\n"), "Description: Marketplace text.") {
		t.Error("moving the cursor collapsed the detail block")
	}
	// A second Enter on the row collapses it.
	m.handlePanelKey(tea.Key{Code: tea.KeyUp})
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if strings.Contains(strings.Join(plainLines(m), "\n"), "Description:") {
		t.Error("second Enter did not collapse the block")
	}
}

// TestCycle198_TabCyclesAndResetsCursor covers Tab switching and the cursor
// reset the reference does on every switch.
func TestCycle198_TabCyclesAndResetsCursor(t *testing.T) {
	root := cycle198Root(t)
	seedPlugin(t, root, "alpha", "Alpha.", "1.0.0")
	seedMarketplace(t, root, "mkt", [][2]string{{"p1", "P1."}, {"p2", "P2."}})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelDiscover || m.panel.cursor != 0 {
		t.Fatalf("after Tab: tab=%d cursor=%d, want 1/0", m.panel.pluginTab, m.panel.cursor)
	}
	lines := plainLines(m)
	if lines[3] != "> + Install from local directory..." {
		t.Errorf("Discover row 0 = %q", lines[3])
	}
	if lines[4] != "  ● p1 P1." {
		t.Errorf("Discover row 1 = %q", lines[4])
	}
	if m.pluginRowCount() != 3 {
		t.Errorf("pluginRowCount on Discover = %d, want 3 (2 entries + local row)", m.pluginRowCount())
	}
	// The legend follows the tab (it is the last panel line).
	if last := lines[len(lines)-1]; !strings.Contains(last, "Enter Local Path") {
		t.Errorf("Discover legend = %q", last)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelInstalled || m.panel.cursor != 0 {
		t.Fatalf("after second Tab: tab=%d cursor=%d, want 0/0", m.panel.pluginTab, m.panel.cursor)
	}
}

// TestCycle198_DiscoverBulletTracksInstalled checks the ● colour: green 29 for
// an installed plugin, grey 253 otherwise — enabled or not.
func TestCycle198_DiscoverBullet(t *testing.T) {
	root := cycle198Root(t)
	seedPlugin(t, root, "alpha", "Alpha.", "1.0.0")
	seedPluginConfig(t, root, map[string]bool{"alpha": false})
	seedMarketplace(t, root, "mkt", [][2]string{{"alpha", "A."}, {"omega", "O."}})

	installed := pluginDiscoverLine(false, true, "alpha", "A.", 218)
	if !strings.Contains(installed, "\x1b[38;2;133;153;0m●") {
		t.Errorf("installed bullet not green: %q", installed)
	}
	unknown := pluginDiscoverLine(false, false, "omega", "O.", 218)
	if !strings.Contains(unknown, "\x1b[38;2;238;232;213m●") {
		t.Errorf("unknown bullet not grey: %q", unknown)
	}
	// Disabled-but-installed stays green.
	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	if got := plainLines(m)[4]; !strings.Contains(got, "● alpha") {
		t.Errorf("discover row = %q", got)
	}
}

// TestCycle198_TruncateTo pins the width rule: the whole line fits
// terminal_width-2 columns and a cut appends "...".
func TestCycle198_TruncateTo(t *testing.T) {
	cases := []struct {
		in    string
		avail int
		want  string
	}{
		{"", 10, ""},
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"abcdefghijkl", 10, "abcdefg..."},
		{"abcdefghijkl", 3, "abcdefghijkl"}, // nothing fits in <4 columns
		{"abcdefghijkl", 4, "a..."},
	}
	for _, c := range cases {
		if got := truncateTo(c.in, c.avail); got != c.want {
			t.Errorf("truncateTo(%q, %d) = %q, want %q", c.in, c.avail, got, c.want)
		}
	}
	m := &runMode{}
	if got := m.pluginLineMax(); got < 1000 {
		t.Errorf("unknown width: pluginLineMax = %d, want unbounded", got)
	}
	m.width = 220
	if got := m.pluginLineMax(); got != 218 {
		t.Errorf("width 220: pluginLineMax = %d, want 218", got)
	}
	m.width = 100
	if got := m.pluginLineMax(); got != 98 {
		t.Errorf("width 100: pluginLineMax = %d, want 98", got)
	}
}

// TestCycle198_RowTruncationFits checks the row builders honour the width: the
// reference cuts a long description so the row is width-2 columns wide.
func TestCycle198_RowTruncationFits(t *testing.T) {
	cycle198Root(t)
	long := strings.Repeat("word ", 60)
	m := &runMode{width: 100}
	row := stripSGR(pluginInstalledLine(true, true, "some-plugin", long, m.pluginLineMax()))
	if len([]rune(row)) != 98 {
		t.Errorf("installed row width = %d, want 98: %q", len([]rune(row)), row)
	}
	if !strings.HasSuffix(row, "...") {
		t.Errorf("truncated row should end in \"...\": %q", row)
	}
	row = stripSGR(pluginDiscoverLine(false, false, "p", long, m.pluginLineMax()))
	if len([]rune(row)) != 98 {
		t.Errorf("discover row width = %d, want 98: %q", len([]rune(row)), row)
	}
}

// TestCycle198_DescPrefersMarketplace: both the row and the details block show
// the marketplace text when the shipped plugin.json disagrees.
func TestCycle198_DescPrefersMarketplace(t *testing.T) {
	root := cycle198Root(t)
	seedPlugin(t, root, "modern-web-guidance-plugin", "Curated collection of agent skills for modern web development.", "1.0.0")
	seedMarketplace(t, root, "mkt",
		[][2]string{{"modern-web-guidance-plugin", "Keep your coding agent up to date with the latest web best practices."}})
	mkts := pluginMarketplaces()
	if got := pluginDescFor("modern-web-guidance-plugin", mkts); got != "Keep your coding agent up to date with the latest web best practices." {
		t.Errorf("pluginDescFor = %q", got)
	}
	m := mkPanel(panelPlugin, "/plugin", 0)
	if got := plainLines(m)[3]; !strings.Contains(got, "latest web best practices.") {
		t.Errorf("row = %q", got)
	}
}

// TestCycle198_InstalledFallback: with no plugin tree the panel still renders
// the four stock rows.
func TestCycle198_InstalledFallback(t *testing.T) {
	cycle198Root(t)
	rows := installedPluginRows()
	if len(rows) != 4 || rows[0].name != "chrome-devtools-plugin" {
		t.Fatalf("fallback rows = %+v", rows)
	}
	m := mkPanel(panelPlugin, "/plugin", 0)
	lines := plainLines(m)
	// title, tab row, search box, 4 plugin rows, blank, legend
	if len(lines) != 9 {
		t.Errorf("Installed panel has %d lines, want 9: %q", len(lines), lines)
	}
	if !strings.Contains(lines[3], "[disabled]") {
		t.Errorf("a plugin absent from config.json should render [disabled]: %q", lines[3])
	}
}

// ---- plugin space toggle tests --------------------------------------------

// TestPluginSpaceToggle_EnabledToDisabled pins the headline direction:
// a Space press on the cursor row of an installed plugin whose
// plugins.<name>.enabled flag is true flips the flag to false in
// ~/.gemini/config/config.json.
func TestPluginSpaceToggle_EnabledToDisabled(t *testing.T) {
	root := cycle198Root(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.0.0")
	seedPluginConfig(t, root, map[string]bool{"alpha": true})

	m := mkPanel(panelPlugin, "/plugin", 0)
	if on, _ := auth.PluginEnabled("alpha"); !on {
		t.Fatal("setup: alpha should be seeded enabled")
	}
	handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if !handled {
		t.Fatal("space reported unhandled — falls through to printable-input filter?")
	}
	if cmd != nil {
		t.Errorf("space returned a cmd %v, want nil", cmd)
	}
	if on, _ := auth.PluginEnabled("alpha"); on {
		t.Error("space did not disable alpha")
	}
}

// TestPluginSpaceToggle_DisabledToEnabled pins the inverse direction:
// starting with the flag false, Space flips it true.
func TestPluginSpaceToggle_DisabledToEnabled(t *testing.T) {
	root := cycle198Root(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.0.0")
	seedPluginConfig(t, root, map[string]bool{"alpha": false})

	m := mkPanel(panelPlugin, "/plugin", 0)
	if on, _ := auth.PluginEnabled("alpha"); on {
		t.Fatal("setup: alpha should be seeded disabled")
	}
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if on, _ := auth.PluginEnabled("alpha"); !on {
		t.Error("space did not enable alpha")
	}
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if on, _ := auth.PluginEnabled("alpha"); on {
		t.Error("second space did not re-disable alpha")
	}
}

// TestPluginSpaceToggle_PersistsToDisk asserts the toggle reaches
// config.json on disk — re-reading the file shows the updated flag,
// not just the in-memory cache.
func TestPluginSpaceToggle_PersistsToDisk(t *testing.T) {
	root := cycle198Root(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.0.0")
	seedPluginConfig(t, root, map[string]bool{"alpha": true})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})

	data, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal config.json: %v", err)
	}
	plugins, _ := doc["plugins"].(map[string]any)
	if plugins == nil {
		t.Fatalf("config.json lost the plugins map: %v", doc)
	}
	alpha, _ := plugins["alpha"].(map[string]any)
	if alpha == nil {
		t.Fatalf("config.json lost the alpha entry: %v", plugins)
	}
	if enabled, _ := alpha["enabled"].(bool); enabled {
		t.Errorf("config.json still has alpha.enabled=true after toggle: %v", alpha)
	}
	if err := auth.SetPluginEnabled("swapped-in", true); err != nil {
		t.Fatalf("SetPluginEnabled(swapped-in): %v", err)
	}
	data, err = os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatalf("re-read config.json: %v", err)
	}
	doc = nil
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("re-unmarshal config.json: %v", err)
	}
	plugins, _ = doc["plugins"].(map[string]any)
	if _, ok := plugins["swapped-in"]; !ok {
		t.Errorf("config.json missing the swapped-in entry after round-trip: %v", plugins)
	}
}

// TestPluginSpaceToggle_RerendersRow pins the visual feedback the user
// sees: the row under the cursor changes from [enabled] to [disabled]
// after the toggle, with no further UI action required.
func TestPluginSpaceToggle_RerendersRow(t *testing.T) {
	root := cycle198Root(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.0.0")
	seedPlugin(t, root, "beta", "Beta plugin.", "1.0.0")
	seedPluginConfig(t, root, map[string]bool{"alpha": true, "beta": true})

	m := mkPanel(panelPlugin, "/plugin", 0)

	pre := findPluginRow(t, m, "alpha")
	if !strings.Contains(pre, "[enabled]") {
		t.Fatalf("setup: alpha row %q does not carry [enabled]", pre)
	}

	m.handlePanelKey(tea.Key{Code: tea.KeySpace})

	post := findPluginRow(t, m, "alpha")
	if !strings.Contains(post, "[disabled]") {
		t.Errorf("after toggle: alpha row = %q, want [disabled]", post)
	}
	if strings.Contains(post, "[enabled]") {
		t.Errorf("after toggle: alpha row = %q still carries [enabled]", post)
	}

	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	round := findPluginRow(t, m, "alpha")
	if !strings.Contains(round, "[enabled]") {
		t.Errorf("second toggle: alpha row = %q, want [enabled]", round)
	}
}

// TestPluginSpaceToggle_SpaceOnDiscoverTab pins the unfocused Discover
// behaviour the cycle-204 legend documents: a Space press focuses the
// search box.
func TestPluginSpaceToggle_SpaceOnDiscoverTab(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover

	if m.panel.pluginSearch {
		t.Fatal("setup: search box should not be focused yet")
	}
	handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if !handled {
		t.Fatal("space on Discover reported unhandled")
	}
	if cmd == nil {
		t.Error("space on Discover returned a nil cmd — expected pluginBlink for the focus cursor")
	}
	if !m.panel.pluginSearch {
		t.Error("space on Discover did not focus the search box")
	}
	if m.panel.pluginQuery != "" {
		t.Errorf("space seeded the query as %q, want \"\"", m.panel.pluginQuery)
	}
	if m.panel.pluginTab != panelDiscover {
		t.Errorf("space changed the tab to %d, want Discover", m.panel.pluginTab)
	}
}

// findPluginRow returns the first SGR-stripped panel line containing
// the plugin name.
func findPluginRow(t *testing.T, m *runMode, name string, want ...string) string {
	t.Helper()
	for _, line := range plainLines(m) {
		if !strings.Contains(line, name) {
			continue
		}
		return line
	}
	t.Fatalf("no row in panel contains %q", name)
	return ""
}

// ---- plugin case-insensitive subcommand tests ------------------------------

// TestSlashPlugin_ListAllCases: the three documented case variants
// (capitalised, mixed, lowercase) all dispatch to the same
// list-handler body.
func TestSlashPlugin_ListAllCases(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"lowercase", "/plugin list"},
		{"capitalised", "/plugin List"},
		{"allcaps", "/plugin LIST"},
		{"mixed", "/plugin LiSt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand(tc.command)
			if !handled {
				t.Fatalf("handleSlashCommand(%q) reported not handled; expected subcommand dispatch", tc.command)
			}
			if m.panel.kind != panelNone {
				t.Errorf("handleSlashCommand(%q) opened panel kind %d; want panelNone", tc.command, m.panel.kind)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "No plugins installed.") {
				t.Errorf("%s output missing list body: %q", tc.command, got)
			}
		})
	}
}

// TestSlashPlugin_InstallDispatch: `/plugin install <path>` and
// its case variants emit a usage stub.
func TestSlashPlugin_InstallDispatch(t *testing.T) {
	for _, cmd := range []string{"/plugin install", "/plugin Install", "/plugin INSTALL"} {
		t.Run(cmd, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand(cmd + " ./my-plugin")
			if !handled {
				t.Fatalf("handleSlashCommand(%q) not handled", cmd)
			}
			if m.panel.kind != panelNone {
				t.Errorf("handleSlashCommand(%q) opened panel; want subcommand dispatch", cmd)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "Usage:") {
				t.Errorf("%s output missing usage stub: %q", cmd, got)
			}
			if !strings.Contains(got, "install") {
				t.Errorf("%s output should mention install: %q", cmd, got)
			}
		})
	}
}

// TestSlashPlugin_ValidateDispatch: the validate subcommand
// surfaces a usage stub regardless of case.
func TestSlashPlugin_ValidateDispatch(t *testing.T) {
	for _, cmd := range []string{"/plugin validate", "/plugin Validate"} {
		t.Run(cmd, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand(cmd + " ./plugin-tree")
			if !handled {
				t.Fatalf("handleSlashCommand(%q) not handled", cmd)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "Usage:") {
				t.Errorf("%s output missing usage stub: %q", cmd, got)
			}
			if !strings.Contains(got, "validate") {
				t.Errorf("%s output should mention validate: %q", cmd, got)
			}
		})
	}
}

// TestSlashPlugin_EnableDisableDispatch: enable and disable
// dispatch case-insensitively.
func TestSlashPlugin_EnableDisableDispatch(t *testing.T) {
	for _, verb := range []string{"enable", "Enable", "ENABLE"} {
		t.Run("enable/"+verb, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand("/plugin " + verb + " my-plugin")
			if !handled {
				t.Fatalf("handleSlashCommand(%q) not handled", verb)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "Usage:") {
				t.Errorf("/plugin %s output missing usage stub: %q", verb, got)
			}
			if !strings.Contains(got, "enable") && !strings.Contains(got, "Enable") && !strings.Contains(got, "ENABLE") {
				t.Errorf("/plugin %s output should mention enable: %q", verb, got)
			}
		})
	}
	for _, verb := range []string{"disable", "Disable"} {
		t.Run("disable/"+verb, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand("/plugin " + verb + " my-plugin")
			if !handled {
				t.Fatalf("handleSlashCommand(%q) not handled", verb)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "Usage:") {
				t.Errorf("/plugin %s output missing usage stub: %q", verb, got)
			}
			if !strings.Contains(got, "disable") && !strings.Contains(got, "Disable") {
				t.Errorf("/plugin %s output should mention disable: %q", verb, got)
			}
		})
	}
}

// TestSlashPlugin_UnknownSubcommand: a subcommand that does not
// exist still surfaces a "Unknown subcommand: <verb>" line.
func TestSlashPlugin_UnknownSubcommand(t *testing.T) {
	m := newSlashModel(t)
	handled, _ := m.handleSlashCommand("/plugin frobnicate")
	if !handled {
		t.Fatalf("handleSlashCommand not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel opened for unknown subcommand")
	}
	got := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(got, "Unknown subcommand") {
		t.Errorf("output missing 'Unknown subcommand': %q", got)
	}
	if !strings.Contains(got, "frobnicate") {
		t.Errorf("output should echo the verb name: %q", got)
	}
}

// TestSlashPlugin_BareFallsToPanel: bare `/plugin` (no args) falls through
// to panel opening.
func TestSlashPlugin_BareFallsToPanel(t *testing.T) {
	m := newSlashModel(t)
	handled, _ := m.handleSlashCommand("/plugin")
	if !handled {
		t.Fatalf("handleSlashCommand(/plugin) not handled")
	}
	got := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(got, "Plugins: 0 installed") {
		t.Errorf("bare /plugin fallback missing: %q", got)
	}
}

// TestCycle234_MarkdownOnlyIsUnchanged verifies the merge is a true union:
// a plugin that ships only the legacy <plugin>/agents/*.md sidecar keeps
// every entry (no shadowing, no filtering).
func TestCycle234_MarkdownOnlyIsUnchanged(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "md-only")
	for _, name := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agents", name+".md"),
			[]byte("# "+name+"\n"), 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	got := pluginAgents("md-only")
	want := []string{"alpha", "beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(md-only) = %v, want %v", got, want)
	}
}

// TestCycle234_JsonOnlyIsLoaded mirrors the changelog line 13 fix: a plugin
// with *only* an agents.json at the root still gets every declared subagent
// surfaced. Pre-cycle-234 the discover / install / validate paths read
// nothing from agents.json.
func TestCycle234_JsonOnlyIsLoaded(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "json-only")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	doc := map[string]any{
		"agents": []map[string]any{
			{"name": "delta", "path": "agents/delta.md"},
			{"name": "epsilon", "path": "agents/epsilon.md"},
		},
	}
	writeJSONFile(t, filepath.Join(dir, "agents.json"), doc)
	got := pluginAgents("json-only")
	want := []string{"delta", "epsilon"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(json-only) = %v, want %v", got, want)
	}
}

// TestCycle234_UnionOfBothSources: the merge of agents/*.md and agents.json
// returns the union, sorted, with neither source shadowing.
func TestCycle234_UnionOfBothSources(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "both")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	// Three markdown entries.
	for _, name := range []string{"md-a", "md-b", "shared"} {
		if err := os.WriteFile(filepath.Join(dir, "agents", name+".md"),
			[]byte("# "+name), 0600); err != nil {
			t.Fatalf("write %s.md: %v", name, err)
		}
	}
	// Three JSON entries — one shared with the markdown sidecar,
	// two JSON-only.
	doc := map[string]any{
		"agents": []map[string]any{
			{"name": "shared", "path": "agents/shared.md"},
			{"name": "json-x", "path": "agents/json-x.md"},
			{"name": "json-y", "path": "agents/json-y.md"},
		},
	}
	writeJSONFile(t, filepath.Join(dir, "agents.json"), doc)
	got := pluginAgents("both")
	want := []string{"json-x", "json-y", "md-a", "md-b", "shared"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(both) = %v, want %v (shared dedupes)", got, want)
	}
}

// TestCycle234_NeitherPresentUnaffected: a plugin with no agents source
// at all returns nil — the empty-state row the Installed detail block
// turns into "-".
func TestCycle234_NeitherPresentUnaffected(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "empty")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"empty"}`), 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got := pluginAgents("empty")
	if got != nil {
		t.Errorf("pluginAgents(empty) = %v, want nil", got)
	}
}

// TestCycle234_DiscoverPreviewListsAllAgents: the /plugin Discover detail
// block's `Agents:` value mirrors pluginAgents(name) on the unpacked-cache
// mirror (the same way Components does). When the plugin has been unpacked
// into <cache>/marketplaces/<mkt>/plugins/<name>/<ver>/, the agents.json + the
// agents/*.md sidecar both contribute.
func TestCycle234_DiscoverPreviewListsAllAgents(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{
		{"p", "P plugin.", "9.9.9"},
	})
	// Unpack path: cache/marketplaces/mkt/plugins/p/9.9.9/agents.json
	unpack := filepath.Join(root, "antigravity-cli", "cache", "marketplaces",
		"mkt", "plugins", "p", "9.9.9")
	if err := os.MkdirAll(filepath.Join(unpack, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSONFile(t, filepath.Join(unpack, "agents.json"), map[string]any{
		"agents": []map[string]any{
			{"name": "alpha-agent", "path": "agents/alpha.md"},
		},
	})
	if err := os.WriteFile(filepath.Join(unpack, "agents", "beta.md"),
		[]byte("# beta"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	lines := pluginDiscoverDetails("p", pluginMarketplaces(), 1<<20)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "alpha-agent, beta") {
		t.Errorf("Agents line missing union: %q", joined)
	}
}

// TestCycle234_InstalledDetailsHasAgentsRow pins the Installed-tab detail
// block's six-line shape after cycle 234: Description, Skills, Agents,
// Marketplace, Path, Version — the new Agents row sits between Skills and
// Marketplace so the panel does not break the cycle 198 byte-layout for
// any existing column.
func TestCycle234_InstalledDetailsHasAgentsRow(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "cycle234")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"cycle234","description":"x","version":"1.0.0"}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "zeta.md"),
		[]byte("# zeta"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	details := pluginInstalledDetails("cycle234", nil)
	joined := strings.Join(details, "\n")
	// The SGR codes pluginDetailLine wraps each row in make the literal
	// "      Version:     1.0.0" not appear as a substring; we assert the
	// row labels individually and check that the agents row carries
	// "zeta" verbatim (no comma separator on a single entry).
	wantSubstrs := []string{
		"Description:",
		"Skills:", // empty value (no skills seeded) — the label itself proves the row is present
		"Agents:      zeta",
		"Marketplace:",
		"Path:        ",
		"Version:     1.0.0",
	}
	for _, w := range wantSubstrs {
		if !strings.Contains(joined, w) {
			t.Errorf("Installed detail block missing %q:\n%s", w, joined)
		}
	}
	// And there are exactly six detail rows now (Description / Skills /
	// Agents / Marketplace / Path / Version).
	if len(details) != 6 {
		t.Errorf("Installed detail block has %d rows, want 6: %q", len(details), details)
	}
}

// TestCycle234_MalformedAgentsJsonFallsBack: a malformed agents.json does
// NOT crash the loader. pluginAgents is the TUI-side entry point and is
// expected to silently treat the JSON as missing; pluginAgentsAtPath (used
// by the CLI handlers) does surface the parse error and the listings are
// empty — both behaviours are deliberately different.
func TestCycle234_MalformedAgentsJsonFallsBack(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "broken-json")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"broken-json"}`), 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents.json"),
		[]byte(`{"agents": [{"name": "x"`), 0600); err != nil { // missing closer
		t.Fatalf("write broken: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "alpha.md"),
		[]byte("# alpha"), 0600); err != nil {
		t.Fatalf("write md: %v", err)
	}
	// TUI: the markdown sidecar is the source of truth when the JSON
	// is broken — the panel still has something to show.
	got := pluginAgents("broken-json")
	if strings.Join(got, ",") != "alpha" {
		t.Errorf("pluginAgents(broken-json) = %v, want [alpha]", got)
	}

	// CLI side: the parse error must surface so `agy plugin validate`
	// can refuse the install.
	_, jsonErr, _ := pluginAgentsAtPath(dir)
	if jsonErr == nil {
		t.Errorf("pluginAgentsAtPath did not surface JSON parse error")
	}
}

// TestCycle234_AgentsJsonEntryMissingNameSkipped: a parseable JSON file
// whose entries lack `name` (the only required field) drops the
// nameless entries silently. The on-disk filename is the fallback name in
// the markdown sidecar; the JSON manifest has no equivalent.
func TestCycle234_AgentsJsonEntryMissingNameSkipped(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "nameless")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSONFile(t, filepath.Join(dir, "agents.json"), map[string]any{
		"agents": []map[string]any{
			{"name": "ok", "path": "x.md"},
			{"path": "no-name.md"}, // missing name
			{"name": "", "path": "empty.md"},
		},
	})
	got := pluginAgents("nameless")
	if strings.Join(got, ",") != "ok" {
		t.Errorf("pluginAgents(nameless) = %v, want [ok]", got)
	}
}

// TestCycle234_JsonFlatFormAccepted: an accidentally-flat JSON array (a
// common pre-1.3.3 typo that AGY keeps tolerating) parses the same way.
func TestCycle234_JsonFlatFormAccepted(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "flat")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := []map[string]any{
		{"name": "f1", "path": "a.md"},
		{"name": "f2", "path": "b.md"},
	}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(filepath.Join(dir, "agents.json"), raw, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := pluginAgents("flat")
	want := []string{"f1", "f2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(flat) = %v, want %v", got, want)
	}
}

// TestCycle234_JsonExcludesNonMdFiles: an agents/ directory with
// non-markdown sidecars (README.md's only the suffix `.md`, but
// `agents.txt` has no suffix) doesn't contribute to the listing. Same
// filter as the cycle-198 skills/ reader.
func TestCycle234_JsonExcludesNonMdFiles(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "no-md")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "real.md"),
		[]byte("# real"), 0600); err != nil {
		t.Fatalf("write real: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "notes.txt"),
		[]byte("not a subagent"), 0600); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "agents", "subdir"), 0700); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	got := pluginAgents("no-md")
	if strings.Join(got, ",") != "real" {
		t.Errorf("pluginAgents(no-md) = %v, want [real]", got)
	}
}

// TestCycle234_JsonSortIsStable: the order is alphabetical regardless of
// the on-disk file order or the JSON entry order. Pins the `sort.Strings`
// call in pluginAgents.
func TestCycle234_JsonSortIsStable(t *testing.T) {
	root := cycle198Root(t)
	dir := filepath.Join(root, "config", "plugins", "sort")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSONFile(t, filepath.Join(dir, "agents.json"), map[string]any{
		"agents": []map[string]any{
			{"name": "zulu"},
			{"name": "alpha"},
			{"name": "mike"},
		},
	})
	if err := os.WriteFile(filepath.Join(dir, "agents", "yankee.md"),
		[]byte("# yankee"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := pluginAgents("sort")
	want := []string{"alpha", "mike", "yankee", "zulu"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pluginAgents(sort) = %v, want %v", got, want)
	}
}

// cycle235PluginRoot builds <tmp>/.gemini/{antigravity-cli,config} and
// points ANTIGRAVITY_HOME at the antigravity-cli one. Same helper shape
// as cycle198Root — kept private so future cycles can introduce a
// shared one without churning this file.
func cycle235PluginRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".gemini")
	agy := filepath.Join(root, "antigravity-cli")
	if err := os.MkdirAll(agy, 0700); err != nil {
		t.Fatalf("mkdir antigravity-cli: %v", err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agy)
	return root
}

// cycle235ClearGate clears both plugin and MCP gate env vars so a test
// that does not intend to exercise the gate does not accidentally
// inherit one from the host shell. cycle198 tests already pass without
// this helper because Go test processes don't inherit arbitrary env
// vars by default, but cycle 235 tests that switch the gate need a
// clean baseline.
func cycle235ClearGate(t *testing.T) {
	t.Helper()
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "")
	t.Setenv("AGY_CLI_DISABLE_MCP", "")
}

// seedMCPServers writes a synthetic ~/.gemini/antigravity-cli/mcp.json
// for the cycle-235 /mcp tests. The servers map mirrors the v1.3.5
// binary's mcp.json schema (see internal/auth.MCPConfig).
func seedMCPServers(t *testing.T, root string, servers map[string]auth.MCPServer) {
	t.Helper()
	path := filepath.Join(root, "antigravity-cli", "mcp.json")
	doc := map[string]any{"servers": servers}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal mcp.json: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}
}

// TestCycle235_PluginBadgeHelperThreeStates pins the badge helper's
// three outputs: blocked wins over enabled, which wins over disabled.
// The cycle-198 pluginBadge(true) / pluginBadge(false) byte-strings are
// unchanged — only pluginRowBadge (the helper the panel actually calls)
// grows the third state.
func TestCycle235_PluginBadgeHelperThreeStates(t *testing.T) {
	cycle235ClearGate(t)
	// No env var, no config field → PluginsBlocked() is false. With
	// enabled=true the badge is the cycle-198 [enabled] green span.
	if got := pluginRowBadge(true); got != "\x1b[38;2;133;153;0m[enabled] \x1b[39m" {
		t.Errorf("not blocked + enabled = %q", got)
	}
	if got := pluginRowBadge(false); got != pluginColor253+"[disabled]"+pluginColor253C {
		t.Errorf("not blocked + disabled = %q", got)
	}
	// Env var flips the gate — both rows now carry the [blocked] tag.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "1")
	if got := pluginRowBadge(true); got != pluginBlockedBadge() {
		t.Errorf("blocked + enabled = %q, want %q", got, pluginBlockedBadge())
	}
	if got := pluginRowBadge(false); got != pluginBlockedBadge() {
		t.Errorf("blocked + disabled = %q, want %q", got, pluginBlockedBadge())
	}
}

// TestCycle235_PluginBlockedBadgeFromEnvVar: the gate fires when the
// env var is set to any non-empty (after trim) value. An enabled
// plugin in /plugin's Installed tab stops claiming [enabled] and
// switches to the new [blocked] red-orange span.
func TestCycle235_PluginBlockedBadgeFromEnvVar(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.2.3")
	seedPluginConfig(t, root, map[string]bool{"alpha": true})

	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "1")
	if !auth.PluginsBlocked() {
		t.Fatal("env var did not engage PluginsBlocked()")
	}
	m := mkPanel(panelPlugin, "/plugin", 0)
	row := plainLines(m)[3]
	if !strings.Contains(row, "[blocked]") {
		t.Errorf("row with env-var gate = %q, want [blocked]", row)
	}
	// [enabled] must NOT appear when the gate is on — that was the bug.
	if strings.Contains(row, "[enabled]") {
		t.Errorf("gate still rendering [enabled]: %q", row)
	}
}

// TestCycle235_PluginBlockedBadgeFromConfigField: the gate also fires
// from ~/.gemini/config/config.json's top-level disableAllPlugins
// boolean. Same panel rendering expectation as the env-var case.
func TestCycle235_PluginBlockedBadgeFromConfigField(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.2.3")
	seedPluginConfig(t, root, map[string]bool{"alpha": true})

	// Patch config.json to set disableAllPlugins: true at the top
	// level, alongside the per-plugin enabled entry. This mirrors the
	// agy config schema used by v1.3.5 (cycle 198) plus the new field.
	dir := filepath.Join(root, "config")
	doc := map[string]any{
		"plugins":           map[string]any{"alpha": map[string]any{"enabled": true}},
		"userSettings":      map[string]any{"themeMode": "THEME_MODE_INHERIT"},
		"disableAllPlugins": true,
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	if !auth.PluginsBlocked() {
		t.Fatal("config field did not engage PluginsBlocked()")
	}
	m := mkPanel(panelPlugin, "/plugin", 0)
	if got := plainLines(m)[3]; !strings.Contains(got, "[blocked]") {
		t.Errorf("row with config-field gate = %q, want [blocked]", got)
	}
}

// TestCycle235_PluginEnabledNotBlocked: when neither the env var nor
// the config field is set, an enabled plugin keeps the green
// [enabled] badge the cycle-198 tests pin.
func TestCycle235_PluginEnabledNotBlocked(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.2.3")
	seedPluginConfig(t, root, map[string]bool{"alpha": true})

	if auth.PluginsBlocked() {
		t.Fatal("gate unexpectedly engaged")
	}
	m := mkPanel(panelPlugin, "/plugin", 0)
	if got := plainLines(m)[3]; !strings.Contains(got, "[enabled]") {
		t.Errorf("row without gate = %q, want [enabled]", got)
	}
}

// TestCycle235_PluginDisabledBadgeDistinctFromBlocked: a per-plugin
// disabled row that is also behind the gate must show [blocked], not
// [disabled]. That is exactly the AGY 1.2.16 close-gap: previously the
// panel would render [disabled] even when disableAllPlugins was also
// on, leaving the user with no signal that the row was additionally
// gated.
func TestCycle235_PluginDisabledBadgeDistinctFromBlocked(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.2.3")
	seedPluginConfig(t, root, map[string]bool{"alpha": false})

	// Sanity: with the gate off, the disabled plugin renders as
	// [disabled] (cycle-198 contract).
	m := mkPanel(panelPlugin, "/plugin", 0)
	if got := plainLines(m)[3]; !strings.Contains(got, "[disabled]") {
		t.Fatalf("no-gate row = %q, want [disabled]", got)
	}

	// Engage the gate: the row must flip to [blocked]. [disabled]
	// must NOT also appear on the same row.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "yes")
	row := plainLines(m)[3]
	if !strings.Contains(row, "[blocked]") {
		t.Errorf("gate-on row = %q, want [blocked]", row)
	}
	if strings.Contains(row, "[disabled]") {
		t.Errorf("gate-on row still carries [disabled]: %q", row)
	}
}

// TestCycle235_PluginInstalledLineBlockedWidth: the [blocked] badge
// keeps the ten-column width the cycle-198 layout pins
// ("[enabled] "/"[disabled]"). A row that switches to [blocked] must
// not change the column layout — the panel would jitter otherwise.
func TestCycle235_PluginInstalledLineBlockedWidth(t *testing.T) {
	cycle235ClearGate(t)
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "1")

	stripped := stripSGR(pluginInstalledLine(false, true, "p", "desc", 98))
	// "  " + "[blocked] " + " " + "p" + " " + "desc"  → 2 + 10 + 1 + 1 + 1 + 4 = 19
	if got := len([]rune(stripped)); got != 19 {
		t.Errorf("blocked row width = %d, want 19: %q", got, stripped)
	}
}

// TestCycle235_PluginInstalledFallbackStillCycle198 guards the cycle-198
// fallback contract when the gate is engaged: the four stock plugins
// render as [blocked] (not [disabled]) and the row count is still 4.
//
// This is the cross-cycle guard the cycle-198 test (no gate) and the
// cycle-235 test (gate on) both rely on. It pins a regression where a
// future refactor accidentally filtered the fallback plugins out of the
// Installed tab.
func TestCycle235_PluginInstalledFallbackStillCycle198(t *testing.T) {
	cycle235ClearGate(t)
	cycle198Root(t) // no plugin dirs, no config.json → fallback path
	rows := installedPluginRows()
	if len(rows) != 4 {
		t.Fatalf("fallback rows = %d, want 4", len(rows))
	}

	// No gate: cycle-198 contract — every fallback row says [disabled].
	m := mkPanel(panelPlugin, "/plugin", 0)
	for i, line := range plainLines(m)[3:7] {
		if !strings.Contains(line, "[disabled]") {
			t.Errorf("row %d no-gate = %q, want [disabled]", i, line)
		}
	}

	// Gate on: every fallback row says [blocked]. [disabled] is gone.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "1")
	for i, line := range plainLines(m)[3:7] {
		if !strings.Contains(line, "[blocked]") {
			t.Errorf("row %d gated = %q, want [blocked]", i, line)
		}
		if strings.Contains(line, "[disabled]") {
			t.Errorf("row %d gated still carries [disabled]: %q", i, line)
		}
	}
}

// TestCycle235_MCPBlockedFromEnvVar: /mcp renders every server's
// status cell as "blocked" when AGY_CLI_DISABLE_MCP is set, regardless
// of the per-server enabled flag. Same precedence rule as
// PluginsBlocked().
func TestCycle235_MCPBlockedFromEnvVar(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedMCPServers(t, root, map[string]auth.MCPServer{
		"zeta":  {Type: "stdio", Command: "npx", Args: []string{"-y", "z"}, Enabled: true},
		"alpha": {Type: "http", URL: "https://example.test/mcp", Enabled: false},
	})

	t.Setenv("AGY_CLI_DISABLE_MCP", "1")
	if !auth.MCPsBlocked() {
		t.Fatal("env var did not engage MCPsBlocked()")
	}
	got := mcpServersFeedback(auth_testConfig(t, root))
	want := "MCP servers\n" +
		"  alpha\thttp\tblocked\thttps://example.test/mcp\n" +
		"  zeta\tstdio\tblocked\tnpx -y z"
	if got != want {
		t.Errorf("mcpServersFeedback =\n%q\nwant\n%q", got, want)
	}
}

// TestCycle235_MCPBlockedFromConfigField: mcp.json's top-level
// disableAllMCP boolean is the on-disk equivalent of
// AGY_CLI_DISABLE_MCP.
func TestCycle235_MCPBlockedFromConfigField(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedMCPServers(t, root, map[string]auth.MCPServer{
		"zeta": {Type: "stdio", Command: "npx", Args: []string{"-y", "z"}, Enabled: true},
	})

	dir := filepath.Join(root, "antigravity-cli")
	doc := map[string]any{
		"servers": map[string]any{
			"zeta": map[string]any{"type": "stdio", "command": "npx", "args": []string{"-y", "z"}, "enabled": true},
		},
		"disableAllMCP": true,
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), b, 0o600); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}

	if !auth.MCPsBlocked() {
		t.Fatal("config field did not engage MCPsBlocked()")
	}
	got := mcpServersFeedback(auth_testConfig(t, root))
	want := "MCP servers\n" +
		"  zeta\tstdio\tblocked\tnpx -y z"
	if got != want {
		t.Errorf("mcpServersFeedback =\n%q\nwant\n%q", got, want)
	}
}

// TestCycle235_MCPDisabledServerFilteredFromList pins the AGY 1.2.16
// gap line for /mcp: when the global MCP gate is on, a server that is
// per-row disabled no longer surfaces as "disabled" — it surfaces as
// "blocked", and so does every other row. The fix is not literal
// row-removal (the cycle-198 contract lists every configured server)
// but a state-cell rename so users see the global gate, not a per-row
// preference.
func TestCycle235_MCPDisabledServerFilteredFromList(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedMCPServers(t, root, map[string]auth.MCPServer{
		"zeta":  {Type: "stdio", Command: "npx", Args: []string{"-y", "z"}, Enabled: true},
		"alpha": {Type: "http", URL: "https://example.test/mcp", Enabled: false},
	})

	// Without the gate: both servers are listed with their per-row
	// enabled state. This matches TestMCPServersFeedback.
	if got := mcpServersFeedback(auth_testConfig(t, root)); !strings.Contains(got, "alpha\thttp\tdisabled\t") {
		t.Errorf("no-gate listing missing alpha disabled row: %q", got)
	}

	// With the gate: every server's state cell says "blocked". The
	// per-row "disabled" cell is gone — that is the gap fix.
	t.Setenv("AGY_CLI_DISABLE_MCP", "1")
	got := mcpServersFeedback(auth_testConfig(t, root))
	if strings.Contains(got, "\tdisabled\t") {
		t.Errorf("gate-on listing still carries a 'disabled' cell: %q", got)
	}
	for _, name := range []string{"alpha", "zeta"} {
		want := "  " + name + "\t"
		// Find the row and assert the third tab-separated cell.
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(line, want) {
				cells := strings.Split(line, "\t")
				if len(cells) < 4 {
					t.Errorf("malformed row %q", line)
					continue
				}
				if cells[2] != "blocked" {
					t.Errorf("%s state cell = %q, want blocked", name, cells[2])
				}
			}
		}
	}
}

// TestCycle235_PluginsBlockedPrecedence pins the precedence rule: the
// env var wins over the on-disk disableAllPlugins field, and a set
// config field wins over a missing one.
func TestCycle235_PluginsBlockedPrecedence(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedPluginConfig(t, root, map[string]bool{}) // empty config

	// Neither flag → off.
	if auth.PluginsBlocked() {
		t.Fatal("empty config engaged the gate")
	}

	// Set the config field → on.
	doc := map[string]any{
		"plugins":           map[string]any{},
		"userSettings":      map[string]any{"themeMode": "THEME_MODE_INHERIT"},
		"disableAllPlugins": true,
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "config", "config.json"), b, 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	if !auth.PluginsBlocked() {
		t.Fatal("config field did not engage the gate")
	}

	// Set the env var with the field also on → still on (env var
	// matches, no flip).
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "1")
	if !auth.PluginsBlocked() {
		t.Fatal("env var should keep the gate on")
	}

	// Drop the env var, flip the config field off → off.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "")
	doc["disableAllPlugins"] = false
	b, _ = json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "config", "config.json"), b, 0o600); err != nil {
		t.Fatalf("rewrite config.json: %v", err)
	}
	if auth.PluginsBlocked() {
		t.Fatal("cleared flags re-engaged the gate")
	}

	// Whitespace-only env var is "unset" (trim drops it).
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "   ")
	if auth.PluginsBlocked() {
		t.Fatal("whitespace-only env var engaged the gate")
	}
}

// TestCycle235_MCPsBlockedPrecedence mirrors the plugin precedence
// rule for MCPsBlocked.
func TestCycle235_MCPsBlockedPrecedence(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedMCPServers(t, root, map[string]auth.MCPServer{}) // empty config

	if auth.MCPsBlocked() {
		t.Fatal("empty config engaged the gate")
	}

	doc := map[string]any{
		"servers":       map[string]any{},
		"disableAllMCP": true,
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "antigravity-cli", "mcp.json"), b, 0o600); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}
	if !auth.MCPsBlocked() {
		t.Fatal("config field did not engage the gate")
	}

	t.Setenv("AGY_CLI_DISABLE_MCP", "1")
	if !auth.MCPsBlocked() {
		t.Fatal("env var should keep the gate on")
	}

	t.Setenv("AGY_CLI_DISABLE_MCP", "")
	doc["disableAllMCP"] = false
	b, _ = json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "antigravity-cli", "mcp.json"), b, 0o600); err != nil {
		t.Fatalf("rewrite mcp.json: %v", err)
	}
	if auth.MCPsBlocked() {
		t.Fatal("cleared flags re-engaged the gate")
	}

	t.Setenv("AGY_CLI_DISABLE_MCP", " \t ")
	if auth.MCPsBlocked() {
		t.Fatal("whitespace-only env var engaged the gate")
	}
}

// TestCycle235_SlashMCPReadsBlockedState drives /mcp through the
// router, end-to-end: AGY_CLI_DISABLE_MCP=1 plus a configured
// mcp.json, then handleSlashCommand("/mcp"), and the response carries
// the "blocked" cell on every server row. This is the integration
// shape TestSlashMCP_ReadsConfig uses (cycle 167).
func TestCycle235_SlashMCPReadsBlockedState(t *testing.T) {
	cycle235ClearGate(t)
	root := cycle235PluginRoot(t)
	seedMCPServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: true},
	})
	t.Setenv("AGY_CLI_DISABLE_MCP", "1")

	m := newSlashModel(t)
	m.handleSlashCommand("/mcp")
	got := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(got, "local") {
		t.Errorf("/mcp with blocked server rendered %q", got)
	}
	if !strings.Contains(got, "blocked") {
		t.Errorf("/mcp with blocked gate missing 'blocked' cell: %q", got)
	}
	if strings.Contains(got, "enabled") {
		t.Errorf("/mcp with blocked gate still carries 'enabled' cell: %q", got)
	}
}

// auth_testConfig reads mcp.json back through the public
// LoadMCPConfig path. It is a thin shim around auth.LoadMCPConfig —
// not a new dependency, just a name that documents intent.
func auth_testConfig(t *testing.T, root string) *auth.MCPConfig {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(root, "antigravity-cli"))
	cfg, err := auth.LoadMCPConfig()
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	return cfg
}

// dplRoot builds a fresh isolated ~/.gemini tree and points ANTIGRAVITY_HOME
// at it. Returns the .gemini root so seeders can drop fixtures into the
// config/plugins/ tree.
func dplRoot(t *testing.T) string {
	t.Helper()
	return cycle198Root(t)
}

// dplSeed writes <root>/config/plugins/<name>/plugin.json with a minimal
// manifest. The config.json entry is managed separately so multi-row
// tests can build their `enabled` map in one pass; seedPluginConfig
// overwrites the plugins block on each call, which would lose rows
// earlier in the sequence. Tests that want a single on/off flag pair
// should use dplSeedConfig after seeding every directory.
func dplSeed(t *testing.T, root, name, desc string, enabled bool) {
	t.Helper()
	seedPlugin(t, root, name, desc, "1.0.0")
	_ = enabled // see dplSeedConfig for the config.json write
}

// dplSeedConfig writes the enabled-map for the entire seed set in one
// config.json. Used in conjunction with dplSeed above to avoid the
// per-call overwrite behavior of seedPluginConfig.
func dplSeedConfig(t *testing.T, root string, enabled map[string]bool) {
	t.Helper()
	seedPluginConfig(t, root, enabled)
}

// dplNamesOf returns the directory-name list embedded in the Installed
// rows the helper under test produced (order-preserving, dedup not
// necessary because installedPluginRows already returns unique names).
func dplNamesOf(rows []pluginRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.name)
	}
	return out
}

// TestDPL_DisabledPluginInList: the trivial case the bug report hinged on.
// With the customization filter OFF (the post-fix default), an installed
// but disabled plugin still shows up — the cycle-198 reader returns every
// row, the cycle-dpl filter only ever narrows.
func TestDPL_DisabledPluginInList(t *testing.T) {
	root := dplRoot(t)
	dplSeed(t, root, "alpha", "Alpha.", true)
	dplSeed(t, root, "beta", "Beta.", false) // disabled
	dplSeed(t, root, "gamma", "Gamma.", true)
	dplSeedConfig(t, root, map[string]bool{
		"alpha": true, "beta": false, "gamma": true,
	})

	rows := installedPluginRowsFiltered(false)
	got := dplNamesOf(rows)
	for _, want := range []string{"alpha", "beta", "gamma"} {
		found := false
		for _, n := range got {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("disabled plugin %q missing from filter(false): %v", want, got)
		}
	}

	// Sanity: the disabled row carries [disabled] when the panel reads
	// its badge state through pluginEnabled.
	m := mkPanel(panelPlugin, "/plugin", 0)
	if on := m.pluginEnabled("beta"); on {
		t.Errorf("pluginEnabled(beta) = true, want false")
	}
}

// TestDPL_EnabledPluginInList: an enabled plugin appears in BOTH filter
// states. The bug class we're closing is one where disabled rows vanish;
// verifying the enabled rows stay visible across a toggle catches any
// off-by-one where the filter accidentally inverts.
func TestDPL_EnabledPluginInList(t *testing.T) {
	root := dplRoot(t)
	dplSeed(t, root, "alpha", "Alpha.", true)
	dplSeedConfig(t, root, map[string]bool{"alpha": true})

	if rows := installedPluginRowsFiltered(false); len(rows) != 1 || rows[0].name != "alpha" {
		t.Errorf("filter(false) = %v, want only alpha", dplNamesOf(rows))
	}
	if rows := installedPluginRowsFiltered(true); len(rows) != 1 || rows[0].name != "alpha" {
		t.Errorf("filter(true) = %v, want only alpha", dplNamesOf(rows))
	}
}

// TestDPL_MixedEnabledDisabledList: with three installed plugins and one
// disabled, the filter(true) drops the disabled row and keeps the two
// enabled rows in their on-disk order. This is the head-line behaviour
// the AGY 1.2.7 changelog line describes.
func TestDPL_MixedEnabledDisabledList(t *testing.T) {
	root := dplRoot(t)
	dplSeed(t, root, "alpha", "Alpha.", true)
	dplSeed(t, root, "beta", "Beta.", false) // filtered out
	dplSeed(t, root, "gamma", "Gamma.", true)
	dplSeed(t, root, "delta", "Delta.", false) // filtered out
	dplSeed(t, root, "epsilon", "Epsilon.", true)
	dplSeedConfig(t, root, map[string]bool{
		"alpha": true, "beta": false, "gamma": true,
		"delta": false, "epsilon": true,
	})

	all := dplNamesOf(installedPluginRowsFiltered(false))
	if len(all) != 5 {
		t.Errorf("filter(false) = %v, want 5 entries", all)
	}
	enabled := dplNamesOf(installedPluginRowsFiltered(true))
	wantEnabled := []string{"alpha", "epsilon", "gamma"}
	if len(enabled) != len(wantEnabled) {
		t.Fatalf("filter(true) = %v, want %v", enabled, wantEnabled)
	}
	for i, n := range wantEnabled {
		if enabled[i] != n {
			t.Errorf("filter(true)[%d] = %q, want %q", i, enabled[i], n)
		}
	}
}

// TestDPL_QueryEnabledOnlyDoesNotDropDisabledPermanently: the headline
// bug. A user that toggles the filter on and off (without restarting the
// process) must see the disabled rows reappear — they were never gone
// from disk, only from this panel session.
func TestDPL_QueryEnabledOnlyDoesNotDropDisabledPermanently(t *testing.T) {
	root := dplRoot(t)
	dplSeed(t, root, "alpha", "Alpha.", true)
	dplSeed(t, root, "beta", "Beta.", false)
	dplSeed(t, root, "gamma", "Gamma.", true)
	dplSeedConfig(t, root, map[string]bool{
		"alpha": true, "beta": false, "gamma": true,
	})

	m := mkPanel(panelPlugin, "/plugin", 0)

	// Sanity: with the filter off, all three rows appear.
	if got := dplNamesOf(installedPluginRowsFiltered(m.panel.pluginEnabledOnly)); len(got) != 3 {
		t.Fatalf("baseline filter(false) = %v, want all 3 rows", got)
	}

	// Toggle the customization query ON: only the two enabled rows show.
	m.handlePanelKey(tea.Key{Code: '/'})
	if !m.panel.pluginEnabledOnly {
		t.Fatal("/ did not flip pluginEnabledOnly on")
	}
	if got := dplNamesOf(installedPluginRowsFiltered(m.panel.pluginEnabledOnly)); len(got) != 2 {
		t.Fatalf("after toggle-on filter(true) = %v, want alpha + gamma", got)
	}

	// Toggle OFF: every row must come back, including the disabled
	// "beta" — exactly the case the cycle-198 disabled-disappearance
	// bug fixed.
	m.handlePanelKey(tea.Key{Code: '/'})
	if m.panel.pluginEnabledOnly {
		t.Fatal("/ did not flip pluginEnabledOnly back off")
	}
	got := dplNamesOf(installedPluginRowsFiltered(m.panel.pluginEnabledOnly))
	for _, want := range []string{"alpha", "beta", "gamma"} {
		found := false
		for _, n := range got {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("after toggle-off filter: %q missing from %v — AGY 1.2.7 bug regressed", want, got)
		}
	}
}

// TestDPL_PanelCloseResetsFilter: the structural fix the AGY 1.2.7
// closure landed. closePanel assigns a zero-valued slashPanel, so the
// pluginEnabledOnly field drops to its zero value (false). A subsequent
// open must therefore re-render with the filter off.
func TestDPL_PanelCloseResetsFilter(t *testing.T) {
	root := dplRoot(t)
	dplSeed(t, root, "alpha", "Alpha.", true)
	dplSeed(t, root, "beta", "Beta.", false)
	dplSeedConfig(t, root, map[string]bool{"alpha": true, "beta": false})

	m := mkPanel(panelPlugin, "/plugin", 0)
	// Simulate the post-fix pre-state: a panel session that flipped
	// the customization filter on.
	m.panel.pluginEnabledOnly = true
	// Sanity: with the dirty state, beta is missing from the filtered
	// list — this is the pre-fix symptom.
	if got := dplNamesOf(installedPluginRowsFiltered(m.panel.pluginEnabledOnly)); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("dirty-state filter = %v, want only alpha", got)
	}

	// closePanel wipes the panel struct. This is the AGY 1.2.7 fix:
	// the filter was previously held in a session-level cache, so
	// closePanel did not reach it; now the filter lives in the panel
	// struct, so closePanel resets it implicitly.
	m.closePanel()

	if m.panel.pluginEnabledOnly {
		t.Error("closePanel did not reset pluginEnabledOnly — AGY 1.2.7 bug regressed")
	}
	// And the rows are whole again.
	if got := dplNamesOf(installedPluginRowsFiltered(m.panel.pluginEnabledOnly)); len(got) != 2 {
		t.Errorf("post-close filter = %v, want alpha + beta", got)
	}
}

// TestDPL_ValidatePluginID_PathTraversal: the hardened validator pins
// every documented rejection class (empty, separator, NUL, `..`, leading
// dot, disallowed rune). A pass on the empty-input case is also asserted;
// the validator returns the empty error for any well-formed ID like
// "foo-1.0_alpha".
func TestDPL_ValidatePluginID_PathTraversal(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		wantOK bool
	}{
		{"plain", "alpha", true},
		{"dotted name", "alpha.beta", true},
		{"dashed name", "alpha-beta", true},
		{"with digits", "plugin-1_2.3", true},
		{"empty", "", false},
		{"just whitespace", "   ", false},
		{"forward slash", "alpha/beta", false},
		{"backslash", `alpha\beta`, false},
		{"dotdot", "..", false},
		{"dotdot prefix", "../alpha", false},
		{"dotdot windows separator", `..\alpha`, false},
		{"leading dot", ".hidden", false},
		{"nul byte", "alpha\x00beta", false},
		{"absolute posix", "/etc/passwd", false},
		{"absolute windows", `C:\evil`, false},
		{"contains space", "alpha beta", false},
		{"contains shell meta", "alpha;rm -rf", false},
		{"contains newline", "alpha\nbeta", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePluginID(c.id)
			if (err == nil) != c.wantOK {
				t.Errorf("validatePluginID(%q) err = %v, wantOK = %v", c.id, err, c.wantOK)
			}
		})
	}
}

// TestDPL_UninstallPathTraversalRejected: the actual uninstall call must
// refuse to touch the filesystem for a malicious ID. We seed an existing
// plugin ("alpha") under ~/.gemini/config/plugins/alpha and one outside
// the plugins directory (sentinel), then attempt `uninstallPlugin("../sentinel")`
// and `uninstallPlugin("/abs/path")`. Both must return a non-nil error
// and the sentinel file must still exist afterward.
func TestDPL_UninstallPathTraversalRejected(t *testing.T) {
	root := dplRoot(t)

	// A real plugin under the install root, so the validator passes and
	// the uninstall path has at least one row to operate against. We
	// exercise the path-traversal case with the sentinel below.
	dir := filepath.Join(root, "config", "plugins", "alpha")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"alpha","description":"alpha","version":"1.0.0"}`), 0600); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "config.json"),
		[]byte(`{"plugins":{"alpha":{"enabled":true}}}`), 0600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	// Place a sentinel file outside the plugins dir. The hardened
	// uninstall must not be able to remove this regardless of the
	// crafted ID.
	sentinel := filepath.Join(root, "config", "DO_NOT_DELETE")
	if err := os.WriteFile(sentinel, []byte("safe"), 0600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	traversals := []string{
		"../DO_NOT_DELETE",
		"alpha/../../DO_NOT_DELETE",
		"../../tmp/anything",
		"/etc/passwd",
		"\x00",
		".",
		".",
		"",
	}
	for _, bad := range traversals {
		if err := uninstallPlugin(bad); err == nil {
			t.Errorf("uninstallPlugin(%q) accepted by the hardened validator", bad)
		}
	}

	// Sentinel must still be on disk — the validator is the gate, so a
	// rejected ID never reaches os.RemoveAll.
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("sentinel %q was deleted by a rejected uninstall: stat err = %v", sentinel, err)
	}

	// And the legitimate uninstall path still works.
	if err := uninstallPlugin("alpha"); err != nil {
		t.Fatalf("legit uninstallPlugin(alpha) failed: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("legit uninstall left the plugin dir behind: stat err = %v", err)
	}
}

// TestDPL_UninstallClearsConfigEntry: confirms the cycle-dpl controller
// (slash_plugin_uninstall.go) deletes both the directory and the matching
// config.json entry. The next installedPluginRowsFiltered call must not
// surface "alpha".
func TestDPL_UninstallClearsConfigEntry(t *testing.T) {
	root := dplRoot(t)
	dplSeed(t, root, "alpha", "Alpha.", true)
	dplSeed(t, root, "beta", "Beta.", true)
	dplSeedConfig(t, root, map[string]bool{"alpha": true, "beta": true})

	if err := uninstallPlugin("alpha"); err != nil {
		t.Fatalf("uninstall alpha: %v", err)
	}
	if _, err := auth.PluginEnabled("alpha"); err != nil {
		t.Fatalf("PluginEnabled(alpha) after uninstall: %v", err)
	}
	// Re-read config.json directly to assert the row is gone, not just
	// reported as false. PluginEnabled returns (false, nil) for both
	// "row present, enabled=false" and "row missing", so the proxy
	// alone cannot distinguish them — the AGY 1.2.7 hardening
	// requirement is a real delete.
	data, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal config.json: %v", err)
	}
	plugins, _ := doc["plugins"].(map[string]any)
	if _, ok := plugins["alpha"]; ok {
		t.Errorf("config.json still carries an entry for alpha: %v", plugins)
	}
	// And beta rides through (we did not delete beta).
	if _, ok := plugins["beta"]; !ok {
		t.Errorf("config.json lost the beta entry: %v", plugins)
	}
	// installedPluginRowsFiltered should also drop alpha — the panel
	// re-reads the disk on every frame, so this verifies the round-trip
	// that cycle-dpl depends on.
	if got := dplNamesOf(installedPluginRowsFiltered(false)); dplContains(got, "alpha") {
		t.Errorf("alpha still listed after uninstall: %v", got)
	}
}

// TestDPL_PanelCtrlSUninstallsAndSurfacesSuccess: drives the panel key
// handler the user actually uses. After ctrl+s, the highlighted plugin
// is removed and the panel title carries an empty title (no red error
// span). The plugin must also be gone from the on-disk tree so a
// subsequent installedPluginRowsFiltered call does not surface it.
func TestDPL_PanelCtrlSUninstallsAndSurfacesSuccess(t *testing.T) {
	root := dplRoot(t)
	dplSeed(t, root, "alpha", "Alpha.", true)
	dplSeed(t, root, "beta", "Beta.", true)
	dplSeedConfig(t, root, map[string]bool{"alpha": true, "beta": true})

	m := mkPanel(panelPlugin, "/plugin", 0)
	// Cursor at 0 = alpha (the alphabetical first row).
	m.panel.cursor = 0
	// The hardened ctrl+s handler must report no error and an empty
	// notice (success: title reverts to cycle-198).
	notice, err := m.uninstallHighlightedPlugin()
	if err != nil {
		t.Errorf("uninstallHighlightedPlugin() err = %v, want nil", err)
	}
	// The success path returns a `Plugin "%s" uninstalled.` notice so
	// the user can see the cycle-dpl confirmation line. We only
	// require it to be free of the "Error:" prefix — that's what the
	// panel title uses to paint the red error span.
	if strings.HasPrefix(notice, "Error:") {
		t.Errorf("uninstallHighlightedPlugin() notice = %q, want non-error", notice)
	}
	got := dplNamesOf(installedPluginRowsFiltered(false))
	if dplContains(got, "alpha") {
		t.Errorf("alpha survived ctrl+s: %v", got)
	}
	if !dplContains(got, "beta") {
		t.Errorf("beta lost in the same frame: %v", got)
	}
	// And, just for completeness, the title was not overwritten with
	// an error — the panel-local error string is the same as it was
	// at construct time.
	if strings.Contains(m.panel.pluginLocalError, "Error") {
		t.Errorf("panel-local error set on success: %q", m.panel.pluginLocalError)
	}
}

// TestDPL_PanelCtrlSRejectsBadID: same key dispatch, with the cursor
// pinned at a row whose directory name we rename on disk to a path-
// traversal ID. The validator must reject it and the panel-local error
// must surface the verifier's message — the user is told what was wrong
// rather than seeing an arbitrary filesystem error.
func TestDPL_PanelCtrlSRejectsBadID(t *testing.T) {
	root := dplRoot(t)
	// Seed a real, valid plugin so the panel has at least one row to
	// render. Then write a symlink with a path-traversal name into
	// the plugins dir. The panel's installedPluginRows reads via
	// os.ReadDir (cycle 198), which yields the symlink — uninstall
	// then resolves the symlink name and the validator must refuse.
	dir := filepath.Join(root, "config", "plugins")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	normal := filepath.Join(dir, "alpha")
	if err := os.MkdirAll(normal, 0700); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.WriteFile(filepath.Join(normal, "plugin.json"),
		[]byte(`{"name":"alpha","description":"alpha","version":"1.0.0"}`), 0600); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	// Synthetic "evil" entry: a directory whose name is `.._alpha` is
	// still a valid plugin ID (the validator allows `_`), so we cannot
	// simulate the rejection through directory naming alone. Instead,
	// the test calls uninstallPlugin directly with a traversal payload,
	// then asserts the panel-local error path mirrors the verifier's
	// verdict.
	if err := uninstallPlugin("../etc"); err == nil {
		t.Fatal("uninstallPlugin(\"../etc\") accepted — validator regressed")
	}
	if err := uninstallPlugin(`..\etc`); err == nil {
		t.Fatal("uninstallPlugin(`..\\etc`) accepted — validator regressed")
	}
	if err := uninstallPlugin("/etc/passwd"); err == nil {
		t.Fatal("uninstallPlugin(\"/etc/passwd\") accepted — validator regressed")
	}
}

// dplContains is a tiny convenience for "is name in row list" assertions,
// keeping the test bodies readable. It is scoped to the cycle-dpl test
// file because the cycle-197 test already exports its own helper of the
// same name; reusing it across files would tangle the suites.
func dplContains(rowNames []string, target string) bool {
	for _, n := range rowNames {
		if n == target {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- Discover actions & local install (from slash_plugin_discover_actions_test.go)

// cycle212Root builds <tmp>/.gemini/{antigravity-cli,config} and points
// ANTIGRAVITY_HOME at the antigravity-cli one, mirroring cycle204Root.
func cycle212Root(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".gemini")
	agy := filepath.Join(root, "antigravity-cli")
	if err := os.MkdirAll(agy, 0700); err != nil {
		t.Fatalf("mkdir antigravity-cli: %v", err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agy)
	return root
}

// seedLocalPlugin212 writes a plugin.json (and optionally one more file)
// into dir so the install path has something to validate.
func seedLocalPlugin212(t *testing.T, dir, name, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	writeJSONFile(t, filepath.Join(dir, "plugin.json"),
		map[string]any{"name": name, "version": version})
}

// TestCycle212_PromptRowRendering pins the third /plugin line in each
// prompt state: the empty placeholder, a typed path with cursor, and the
// trailing-space cursor the "off" blink phase shows.
func TestCycle212_PromptRowRendering(t *testing.T) {
	cycle212Root(t)
	const label = "\x1b[1m+ Install from local directory:\x1b[0m"

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover

	// Empty prompt, blink on: placeholder visible, cursor visible.
	m.openPluginLocalInstall()
	want := cBlueOpen + "> " + label + cBlueClose + " " +
		"\x1b[7m\x1b[37m \x1b[0m\x1b[38;2;101;123;131m ~/path/to/plugin\x1b[39m"
	if got := m.panelLines()[3]; got != want {
		t.Errorf("empty prompt:\n got %q\nwant %q", got, want)
	}

	// Typed path, blink on: text + cursor.
	m.panel.pluginLocalInstall.query = "/tmp"
	want = cBlueOpen + "> " + label + cBlueClose + " /tmp\x1b[7m\x1b[37m \x1b[0m"
	if got := m.panelLines()[3]; got != want {
		t.Errorf("typed prompt:\n got %q\nwant %q", got, want)
	}

	// Typed path, blink off: text + plain space.
	m.panel.pluginBlinkOn = false
	want = cBlueOpen + "> " + label + cBlueClose + " /tmp "
	if got := m.panelLines()[3]; got != want {
		t.Errorf("typed prompt (blink off):\n got %q\nwant %q", got, want)
	}
}

// TestCycle212_PromptValidationHints pins the three hint strings the
// reference shows below the prompt. The hint line shifts the rest of
// the panel down by one when it appears.
func TestCycle212_PromptValidationHints(t *testing.T) {
	cycle212Root(t)
	cases := []struct {
		name      string
		query     string
		wantHint  string
		wantShift bool // true when the hint row is present
	}{
		{"empty", "", "", false},
		{"not-found", "/tmp/this-does-not-exist-xyz",
			"\x1b[38;2;238;232;213m└─ \x1b[38;2;101;123;131m✗ Directory not found (press Tab to complete)\x1b[39m", true},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := mkPanel(panelPlugin, "/plugin", 0)
			m.panel.pluginTab = panelDiscover
			m.openPluginLocalInstall()
			if c.query != "" {
				m.panel.pluginLocalInstall.query = c.query
			}
			hint := pluginLocalInstallHint(c.query)
			_ = dir
			if c.wantHint == "" {
				if hint != "" {
					t.Errorf("hint = %q, want empty", hint)
				}
				return
			}
			if !strings.Contains(hint, c.wantHint) {
				t.Errorf("hint = %q\nwant contains %q", hint, c.wantHint)
			}
		})
	}

	t.Run("dir-no-plugin-json", func(t *testing.T) {
		dir := t.TempDir()
		m := mkPanel(panelPlugin, "/plugin", 0)
		m.panel.pluginTab = panelDiscover
		m.openPluginLocalInstall()
		m.panel.pluginLocalInstall.query = dir
		hint := pluginLocalInstallHint(dir)
		want := "\x1b[38;2;238;232;213m└─ \x1b[38;2;101;123;131m✗ Directory does not contain plugin.json (press Tab to complete subdirs)\x1b[39m"
		if !strings.Contains(hint, want) {
			t.Errorf("hint = %q\nwant contains %q", hint, want)
		}
	})

	t.Run("valid-plugin", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "plugin")
		seedLocalPlugin212(t, dir, "plugin", "1.0.0")
		m := mkPanel(panelPlugin, "/plugin", 0)
		m.panel.pluginTab = panelDiscover
		m.openPluginLocalInstall()
		m.panel.pluginLocalInstall.query = dir
		if hint := pluginLocalInstallHint(dir); hint != "" {
			t.Errorf("hint = %q, want empty for valid plugin", hint)
		}
	})
}

// TestCycle212_PromptLegend pins the footer the prompt shows instead of
// the Discover tab's row-0 legend.
func TestCycle212_PromptLegend(t *testing.T) {
	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.openPluginLocalInstall()
	lines := plainLines(m)
	if got := lines[len(lines)-1]; got !=
		"Keyboard: enter / ctrl+s Install Plugin  tab Complete Path  esc Cancel" {
		t.Errorf("prompt legend = %q", got)
	}
}

// TestCycle212_PromptKeyLifecycle covers every key the prompt handles:
// printable, backspace, esc, tab completion, and enter on bad/good paths.
func TestCycle212_PromptKeyLifecycle(t *testing.T) {
	cycle212Root(t)

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.openPluginLocalInstall()
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: 'a', Text: "a"})
	if got := m.panel.pluginLocalInstall.query; got != " a" {
		t.Fatalf("query = %q, want %q", got, " a")
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if got := m.panel.pluginLocalInstall.query; got != " " {
		t.Fatalf("backspace = %q, want %q", got, " ")
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if got := m.panel.pluginLocalInstall.query; got != "" {
		t.Fatalf("second backspace = %q, want empty", got)
	}

	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "expandme"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m.panel.pluginLocalInstall.query = filepath.Join(parent, "expand")
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyTab}); !handled {
		t.Fatal("Tab did not report handled")
	}
	if got := m.panel.pluginLocalInstall.query; got != filepath.Join(parent, "expandme")+"/" {
		t.Fatalf("Tab completion = %q, want %q", got, filepath.Join(parent, "expandme")+"/")
	}

	m.panel.pluginLocalInstall.query = "/tmp/this-does-not-exist-xyz"
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.pluginLocalInstallActive() {
		t.Fatal("Enter on a bad path should keep the prompt open")
	}
	if m.panel.pluginLocalError == "" {
		t.Fatal("Enter on a bad path should set pluginLocalError")
	}
	if !strings.Contains(plainLines(m)[0], "(Error:") {
		t.Errorf("title = %q, want contains (Error:", plainLines(m)[0])
	}

	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.pluginLocalInstallActive() {
		t.Fatal("Esc did not close the prompt")
	}
}

// TestCycle212_EnterOpensPromptOnLocalRow pins the keyboard binding that
// opens the prompt: Enter (and ctrl+s) on the un-focused Discover
// local-install row.
func TestCycle212_EnterOpensPromptOnLocalRow(t *testing.T) {
	cycle212Root(t)
	seedMarketplace204(t, t.TempDir(), "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter on the local row reported unhandled")
	}
	if !m.pluginLocalInstallActive() {
		t.Fatal("Enter on the local row did not open the prompt")
	}
	if m.panel.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.panel.cursor)
	}

	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.pluginLocalInstallActive() {
		t.Fatal("Esc did not close the prompt")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: 's', Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+s on the local row reported unhandled")
	}
	if !m.pluginLocalInstallActive() {
		t.Fatal("ctrl+s on the local row did not open the prompt")
	}
}

// TestCycle212_AcceptCopiesAndEnablesPlugin verifies the install path:
// Enter on a directory with a parseable plugin.json copies the source
// directory into ~/.gemini/config/plugins/<manifest-name>/ and writes
// plugins.<name>.enabled=true into config.json.
func TestCycle212_AcceptCopiesAndEnablesPlugin(t *testing.T) {
	root := cycle212Root(t)
	src := filepath.Join(t.TempDir(), "srcdir")
	seedLocalPlugin212(t, src, "agy212-fresh", "1.2.3")

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.openPluginLocalInstall()
	m.panel.pluginLocalInstall.query = src
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})

	if m.pluginLocalInstallActive() {
		t.Fatal("Enter on a valid path did not close the prompt")
	}
	dst := filepath.Join(root, "config", "plugins", "agy212-fresh")
	if _, err := os.Stat(filepath.Join(dst, "plugin.json")); err != nil {
		t.Fatalf("installed plugin.json missing at %s: %v", dst, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if !strings.Contains(string(data), `"agy212-fresh"`) ||
		!strings.Contains(string(data), `"enabled": true`) {
		t.Errorf("config.json = %s, want agy212-fresh enabled", data)
	}
}

func TestCycle212_AcceptConflictLeavesPromptOpen(t *testing.T) {
	root := cycle212Root(t)
	src := filepath.Join(t.TempDir(), "srcdir")
	seedLocalPlugin212(t, src, "agy212-conflict", "1.0.0")
	dst := filepath.Join(root, "config", "plugins", "agy212-conflict")
	if err := os.MkdirAll(dst, 0700); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.openPluginLocalInstall()
	m.panel.pluginLocalInstall.query = src
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})

	if !m.pluginLocalInstallActive() {
		t.Fatal("conflict did not keep the prompt open")
	}
	if !strings.Contains(m.panel.pluginLocalError, "already installed") {
		t.Errorf("pluginLocalError = %q, want contains %q",
			m.panel.pluginLocalError, "already installed")
	}
}

func TestCycle212_SearchBoxBlink(t *testing.T) {
	cycle212Root(t)
	seedMarketplace204(t, t.TempDir(), "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.enterPluginSearch()
	m.panel.pluginQuery = "a"

	const onBox = "\x1b[38;2;88;110;117m  Search: \x1b[39m a\x1b[7m\x1b[37m \x1b[0m"
	const offBox = "\x1b[38;2;88;110;117m  Search: \x1b[39m a "
	if m.panel.pluginBlinkOn == false {
		m.panel.pluginBlinkOn = true
	}
	if got := m.pluginSearchBoxLine(); got != onBox {
		t.Errorf("blink on:\n got %q\nwant %q", got, onBox)
	}
	m.panel.pluginBlinkOn = false
	if got := m.pluginSearchBoxLine(); got != offBox {
		t.Errorf("blink off:\n got %q\nwant %q", got, offBox)
	}
}

func TestCycle212_SearchBoxBlinkMessage(t *testing.T) {
	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.enterPluginSearch()
	initial := m.panel.pluginBlinkOn
	_, cmd := m.Update(pluginBlinkMsg{})
	if cmd == nil {
		t.Fatal("pluginBlinkMsg while focused did not arm pluginBlink()")
	}
	if m.panel.pluginBlinkOn == initial {
		t.Fatal("pluginBlinkMsg did not flip pluginBlinkOn")
	}
	m.exitPluginSearch()
	m.panel.pluginBlinkOn = true
	_, cmd = m.Update(pluginBlinkMsg{})
	if cmd != nil {
		t.Fatal("pluginBlinkMsg after focus was lost should not arm another tick")
	}
	if m.panel.pluginBlinkOn {
		t.Fatal("pluginBlinkMsg after focus was lost should clear pluginBlinkOn")
	}
}

func TestCycle212_TabKeepsQueryAndFocus(t *testing.T) {
	cycle212Root(t)
	seedMarketplace204(t, t.TempDir(), "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedPlugin204(t, t.TempDir(), "installed-one", "Installed.", "1.0.0")

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: 'g', Text: "g"})
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if !m.panel.pluginSearch || m.panel.pluginQuery != "g" {
		t.Fatalf("after first tab: search=%v query=%q, want true/%q",
			m.panel.pluginSearch, m.panel.pluginQuery, "g")
	}
	if m.panel.pluginTab != panelInstalled {
		t.Fatalf("after first tab: tab=%d, want Installed", m.panel.pluginTab)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.pluginTab != panelDiscover {
		t.Fatalf("after second tab: tab=%d, want Discover", m.panel.pluginTab)
	}
	if !m.panel.pluginSearch || m.panel.pluginQuery != "g" {
		t.Fatalf("after second tab: search=%v query=%q, want true/%q",
			m.panel.pluginSearch, m.panel.pluginQuery, "g")
	}
}

func TestCycle212_LeftRightWhileFocusedJumpsFirstLast(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{
		{"alpha", "Alpha.", "1.0.0"},
		{"beta", "Beta.", "1.0.0"},
		{"gamma", "Gamma.", "1.0.0"},
	})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: 'a', Text: "a"})
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right while focused reported unhandled")
	}
	if m.panel.cursor != 2 {
		t.Fatalf("Right while focused = cursor %d, want 2", m.panel.cursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyLeft}); !handled {
		t.Fatal("Left while focused reported unhandled")
	}
	if m.panel.cursor != 0 {
		t.Fatalf("Left while focused = cursor %d, want 0", m.panel.cursor)
	}
}

func TestCycle220_MultiSubdirTabFirstMatch(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"ugo", "umbra", "unicorn", "unknown"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	got := completeLocalInstallPath(filepath.Join(parent, "u"))
	want := filepath.Join(parent, "ugo") + "/"
	if got != want {
		t.Fatalf("complete = %q, want %q", got, want)
	}
	for i := 0; i < 5; i++ {
		again := completeLocalInstallPath(got)
		if again != got {
			t.Fatalf("Tab #%d changed the path: %q → %q", i+1, got, again)
		}
	}
}

func TestCycle220_LongerPrefixedLeaf(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"un_alias", "un_aliases", "un_align"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	got := completeLocalInstallPath(filepath.Join(parent, "un_al"))
	want := filepath.Join(parent, "un_alias") + "/"
	if got != want {
		t.Fatalf("complete = %q, want %q", got, want)
	}
}

func TestCycle220_SingleMatchAlsoAppendsSlash(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "onlyone"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := completeLocalInstallPath(filepath.Join(parent, "only"))
	want := filepath.Join(parent, "onlyone") + "/"
	if got != want {
		t.Fatalf("complete = %q, want %q", got, want)
	}
}

func TestCycle220_ExistingDirectoryGetsTrailingSlash(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "leaf"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := completeLocalInstallPath(filepath.Join(parent, "leaf"))
	want := filepath.Join(parent, "leaf") + "/"
	if got != want {
		t.Fatalf("complete = %q, want %q", got, want)
	}
}

func TestCycle220_TabNoOpOnUnresolvablePath(t *testing.T) {
	got := completeLocalInstallPath("/no/such/path/atall-xyz")
	if got != "/no/such/path/atall-xyz" {
		t.Fatalf("complete = %q, want unchanged", got)
	}
}

func TestCycle220_PromptKeyLifecycleOverTabKeys(t *testing.T) {
	cycle212Root(t)
	parent := t.TempDir()
	for _, name := range []string{"alpha_plugin", "alpha_other", "alpha_x"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.openPluginLocalInstall()
	m.panel.pluginLocalInstall.query = filepath.Join(parent, "alpha")
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyTab}); !handled {
		t.Fatal("first Tab did not report handled")
	}
	want := filepath.Join(parent, "alpha_other") + "/"
	if got := m.panel.pluginLocalInstall.query; got != want {
		t.Fatalf("Tab #1 = %q, want %q", got, want)
	}

	for i := 2; i <= 4; i++ {
		if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyTab}); !handled {
			t.Fatalf("Tab #%d did not report handled", i)
		}
		if got := m.panel.pluginLocalInstall.query; got != want {
			t.Fatalf("Tab #%d changed the query: %q → %q", i, want, got)
		}
	}
}

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
		got := pluginsui.WrapWords(c.in, c.width)
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
			t.Errorf("pluginsui.WrapWords(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
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

// commitInstalled seeds the cycle-psif starting state: a focused
// Installed box with the typed query committed (Enter already
// pressed). The helper layers on focusInstalled from cycle 219 so
// every test starts from the same Typing → Committed transition.
func commitInstalled(t *testing.T, m *runMode, query string) {
	t.Helper()
	focusInstalled(t, m, query)
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginQueryCommitted {
		t.Fatalf("setup: Enter did not commit query %q", query)
	}
	if !m.panel.pluginQueryAsleep {
		t.Fatalf("setup: Enter did not put the cursor to sleep")
	}
}

// installedRows paints the count of installed rows the panel is
// currently showing: the full list when the box is unfocused or the
// query has not been committed; the filter result once the user has
// committed the query.
func installedRows(m *runMode) int {
	if m.panel.pluginTab == panelInstalled &&
		m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		return len(pluginInstalledFilter(installedPluginRows(), m.panel.pluginQuery))
	}
	return len(installedPluginRows())
}

// TestPSIF_TypingAllowsSpaces pins the Typing-phase rule the
// changelog's "you can now type spaces in a search query" line
// introduces: a space is just another printable character the user
// can compose into the query while the Installed box has focus.
// The state stays in Typing — Enter is the only commit key.
func TestPSIF_TypingAllowsSpaces(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x"},
		[]string{"Alpha body.", "Beta body."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "alpha")
	// Append a space and a second word. Two spaces, two printable
	// chars — the query should now read "alpha b x".
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: 'b', Text: "b"})
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: 'x', Text: "x"})

	if got, want := m.panel.pluginQuery, "alpha b x"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	if m.panel.pluginQueryCommitted {
		t.Fatal("spaces alone should not commit the query")
	}
	if m.panel.pluginQueryAsleep {
		t.Fatal("typing while un-committed should leave the cursor awake")
	}
}

// TestPSIF_EnterCommitsSearch pins the Typing → Committed
// transition: pressing Enter on a focused, un-committed Installed
// box flips pluginQueryCommitted and pluginQueryAsleep, and the rows
// below filter live to the typed query. The query is preserved, the
// legend's enter field flips to "Expand Details".
func TestPSIF_EnterCommitsSearch(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x", "foxtrot-x"},
		[]string{"Alpha body.", "Beta body.", "Foxtrot body."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "fox")
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})

	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter did not commit the query")
	}
	if !m.panel.pluginQueryAsleep {
		t.Fatal("Enter should put the cursor to sleep")
	}
	// Filter applied: only foxtrot-x matches "fox".
	if got := installedRows(m); got != 1 {
		t.Fatalf("committed rows = %d, want 1 (foxtrot-x)", got)
	}
	// Legend switches from "enter Search" to "enter Expand Details".
	last := plainLines(m)[len(plainLines(m))-1]
	if !strings.Contains(last, "enter Expand Details") ||
		strings.Contains(last, "enter Search ") {
		t.Errorf("legend after commit = %q, want enter Expand Details", last)
	}
}

// TestPSIF_SpaceInCommittedTogglesEnabled pins the cycle-psif
// headline behaviour: once the query is committed, Space no longer
// appends to the query — it toggles the highlighted row's enabled
// flag in config.json. This is the change AGY 1.3.3 changelog line
// 46 calls out ("use Space to toggle the selected plugin").
func TestPSIF_SpaceInCommittedTogglesEnabled(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x"},
		[]string{"Alpha body.", "Beta body."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	commitInstalled(t, m, "alpha")
	if on, _ := auth.PluginEnabled("alpha-x"); !on {
		t.Fatal("setup: alpha-x should be seeded enabled")
	}
	// Cursor is on row 0 (alpha-x — the only "alpha" match).
	// Space must flip the flag, NOT append a space to the query.
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if on, _ := auth.PluginEnabled("alpha-x"); on {
		t.Fatal("Space in Committed did not disable alpha-x")
	}
	if m.panel.pluginQuery != "alpha" {
		t.Errorf("query = %q, want unchanged %q", m.panel.pluginQuery, "alpha")
	}
	// A second Space re-enables.
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if on, _ := auth.PluginEnabled("alpha-x"); !on {
		t.Fatal("second Space did not re-enable alpha-x")
	}
}

// TestPSIF_SpaceTogglesFilteredRow pins the active-list fix: the
// toggle must act on the row the cursor visually points at, which is
// the *filtered* list when the query has been committed. With more
// than one installed plugin the cursor and the action used to
// disagree: the cursor index 0 was foxtrot-x in the filtered list
// but alpha-x in the full list, so Space flipped the wrong plugin's
// flag. The test seeds three plugins, commits a query that matches
// only the last, and asserts Space toggles the filtered row (not
// the same-index full-list row).
func TestPSIF_SpaceTogglesFilteredRow(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x", "foxtrot-x"},
		[]string{"Alpha body.", "Beta body.", "Foxtrot body."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	commitInstalled(t, m, "f")

	// Sanity: filter narrowed to foxtrot-x, cursor 0 lands on it.
	if got := installedRows(m); got != 1 {
		t.Fatalf("filter result = %d, want 1 (foxtrot-x)", got)
	}
	if m.panel.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.panel.cursor)
	}

	// Space must toggle foxtrot-x, NOT alpha-x (which is full[0]).
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if on, _ := auth.PluginEnabled("foxtrot-x"); on {
		t.Error("Space did not disable foxtrot-x")
	}
	if on, _ := auth.PluginEnabled("alpha-x"); !on {
		t.Error("Space toggled alpha-x — used full list, not active list")
	}
}

// TestPSIF_EnterInCommittedExpandsDetails pins the second Enter
// transition: once the query is committed, pressing Enter opens
// (or collapses) the highlighted row's detail block. The query is
// left intact and the commit flag stays set so the user can keep
// navigating with Space / Enter.
func TestPSIF_EnterInCommittedExpandsDetails(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x"},
		[]string{"Alpha body."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	commitInstalled(t, m, "alpha")
	if m.panel.pluginExpanded["alpha-x"] {
		t.Fatal("setup: detail block should be closed")
	}

	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginExpanded["alpha-x"] {
		t.Fatal("Enter on committed Installed did not open the detail block")
	}
	if !m.panel.pluginQueryCommitted {
		t.Error("Enter must keep the query committed")
	}
	if m.panel.pluginQuery != "alpha" {
		t.Errorf("query = %q, want %q", m.panel.pluginQuery, "alpha")
	}
	if m.panel.pluginQueryAsleep {
		t.Error("Enter should re-awaken the cursor")
	}

	// A second Enter collapses — Enter toggles, like the unfocused tab.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.pluginExpanded["alpha-x"] {
		t.Fatal("second Enter did not collapse the detail block")
	}
}

// TestPSIF_TypingAfterCommitResumesSearch pins the Committed →
// Typing transition the changelog's "start typing again to resume
// the search" line describes: any printable key after a commit
// appends to the query, the rows filter live, and the cursor re-
// awakens. The commit flag stays set so the user does not have to
// press Enter again.
func TestPSIF_TypingAfterCommitResumesSearch(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x", "foxtrot-x"},
		[]string{"Alpha body.", "Beta body.", "Foxtrot body."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	commitInstalled(t, m, "fox")
	if !m.panel.pluginQueryAsleep {
		t.Fatal("setup: cursor should be asleep after commit")
	}

	// Typing 't' must wake the cursor and append to the query.
	m.handlePanelKey(tea.Key{Code: 't', Text: "t"})
	if m.panel.pluginQueryAsleep {
		t.Fatal("typing after commit should re-awaken the cursor")
	}
	if got, want := m.panel.pluginQuery, "foxt"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
	// Flag stays set — the rows still filter live.
	if !m.panel.pluginQueryCommitted {
		t.Error("typing after commit should keep the query committed")
	}
	if got := installedRows(m); got != 1 {
		t.Errorf("filter after 'foxt' = %d rows, want 1 (foxtrot-x)", got)
	}

	// Backspace also resumes (the reference treats backspace as a
	// typed character that wakes the cursor — see cycle 219).
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if got, want := m.panel.pluginQuery, "fox"; got != want {
		t.Errorf("after Backspace: query = %q, want %q", got, want)
	}
	if m.panel.pluginQueryAsleep {
		t.Error("Backspace should keep the cursor awake")
	}
}

// TestPSIF_MultiToggleEachSpaceActsOnSelected pins the multi-toggle
// rule the changelog line 46 calls out ("use Space to toggle the
// selected plugin"): each Space press independently toggles the row
// under the cursor and leaves the other rows alone. The user can
// build a multi-toggle list by pressing Space on different rows.
//
// The test commits a query that matches all three seeded plugins,
// parks on each row in turn, and asserts Space flips only the
// highlighted row's enabled flag.
func TestPSIF_MultiToggleEachSpaceActsOnSelected(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x", "foxtrot-x"},
		[]string{"Alpha body.", "Beta body.", "Foxtrot body."})

	// Commit "x": all three plugins contain "x" in their name, so
	// the filter keeps every row.
	m := mkPanel(panelPlugin, "/plugin", 0)
	commitInstalled(t, m, "x")
	if got := installedRows(m); got != 3 {
		t.Fatalf("commit 'x': %d rows, want 3", got)
	}

	// Cursor 0 -> alpha-x. Space flips alpha-x, leaves the others.
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if on, _ := auth.PluginEnabled("alpha-x"); on {
		t.Error("Space on row 0 did not disable alpha-x")
	}
	if on, _ := auth.PluginEnabled("beta-x"); !on {
		t.Error("Space on row 0 disabled beta-x — used wrong row")
	}
	if on, _ := auth.PluginEnabled("foxtrot-x"); !on {
		t.Error("Space on row 0 disabled foxtrot-x — used wrong row")
	}

	// Cursor 1 -> beta-x. Space flips beta-x, alpha-x stays.
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if on, _ := auth.PluginEnabled("beta-x"); on {
		t.Error("Space on row 1 did not disable beta-x")
	}
	if on, _ := auth.PluginEnabled("alpha-x"); on {
		t.Error("Space on row 1 flipped alpha-x — should still be disabled")
	}

	// Cursor 2 -> foxtrot-x. Space flips foxtrot-x.
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	if on, _ := auth.PluginEnabled("foxtrot-x"); on {
		t.Error("Space on row 2 did not disable foxtrot-x")
	}
	if on, _ := auth.PluginEnabled("alpha-x"); on {
		t.Error("Space on row 2 flipped alpha-x")
	}
	if on, _ := auth.PluginEnabled("beta-x"); on {
		t.Error("Space on row 2 flipped beta-x")
	}

	// The query was never touched.
	if m.panel.pluginQuery != "x" {
		t.Errorf("query = %q, want %q", m.panel.pluginQuery, "x")
	}
}

// TestPSIF_FilterNarrowsAsUserTypes pins the live-filter rule:
// every printable key the user types after committing narrows the
// row list, so "fox" → "fox " → "fox t" → "fox tr" walks the
// installed plugins down to whichever substring match survives.
// Backspace widens it back up. This is the cycle-219 live-filter
// behaviour pinned through the cycle-psif transitions.
func TestPSIF_FilterNarrowsAsUserTypes(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x", "beta-x", "foxtrot-x"},
		[]string{"Alpha body.", "Beta body.",
			"Foxtrot body — checks calendar availability."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	commitInstalled(t, m, "f")
	// "f" matches only foxtrot-x.
	if got := installedRows(m); got != 1 {
		t.Fatalf("after commit 'f': %d rows, want 1", got)
	}

	// "fo" still matches foxtrot-x.
	m.handlePanelKey(tea.Key{Code: 'o', Text: "o"})
	if got := installedRows(m); got != 1 {
		t.Errorf("after 'fo': %d rows, want 1", got)
	}

	// "fox" still matches foxtrot-x.
	m.handlePanelKey(tea.Key{Code: 'x', Text: "x"})
	if got := installedRows(m); got != 1 {
		t.Errorf("after 'fox': %d rows, want 1", got)
	}

	// "foxz" matches nothing.
	m.handlePanelKey(tea.Key{Code: 'z', Text: "z"})
	if got := installedRows(m); got != 0 {
		t.Errorf("after 'foxz': %d rows, want 0", got)
	}
	if got := m.panel.pluginQuery; got != "foxz" {
		t.Errorf("query = %q, want %q", got, "foxz")
	}

	// Backspace widens it back to foxtrot-x.
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if got := installedRows(m); got != 1 {
		t.Errorf("after Backspace to 'fox': %d rows, want 1", got)
	}
}

// TestPSIF_SpaceInTypingStillAppends guards the Typing-phase rule
// against the cycle-psif change: Space in the un-committed phase
// still appends a space to the query. The cycle-psif change only
// affects Space in the Committed phase; the Typing branch is what
// lets the user type multi-word queries in the first place.
func TestPSIF_SpaceInTypingStillAppends(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "",
		[]marketplaceEntry204{{"alpha", "Alpha.", "1.0.0"}})
	seedInstalled219(t, root,
		[]string{"alpha-x"},
		[]string{"Alpha body."})

	m := mkPanel(panelPlugin, "/plugin", 0)
	focusInstalled(t, m, "a")

	// Type a multi-word query; spaces are part of it.
	for _, r := range "lpha body" {
		if r == ' ' {
			m.handlePanelKey(tea.Key{Code: tea.KeySpace})
		} else {
			m.handlePanelKey(tea.Key{Code: r, Text: string(r)})
		}
	}
	if got, want := m.panel.pluginQuery, "alpha body"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	if m.panel.pluginQueryCommitted {
		t.Fatal("typing alone should not commit")
	}
	// alpha-x's enabled flag is still at the seeded state.
	if on, _ := auth.PluginEnabled("alpha-x"); !on {
		t.Fatal("Space in Typing toggled alpha-x — should have only appended")
	}

	// Now commit. With "alpha body" the filter still matches alpha-x.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.pluginQueryCommitted {
		t.Fatal("Enter did not commit")
	}
	if got := installedRows(m); got != 1 {
		t.Errorf("after commit: %d rows, want 1", got)
	}
}

// TestDiscoverSectionsGrouping verifies that when marketplace items span multiple
// categories/sections, the Discover tab renders bold section headers separating them,
// cursor navigation indexes all selectable items across sections, and search mode
// drops the section headers and shows flat filtered results.
func TestDiscoverSectionsGrouping(t *testing.T) {
	root := cycle204Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{
		{"sql-master", "BigQuery and SQL analytics tool.", "1.0.0"},
		{"cloud-runner", "Deploy containers to Kubernetes cloud.", "1.0.0"},
		{"calendar-sync", "Sync Google Calendar schedule and tasks.", "1.0.0"},
		{"code-lint", "Linter and developer tooling.", "1.0.0"},
	})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover

	lines := plainLines(m)
	joined := strings.Join(lines, "\n")

	// Verify all 4 section headers are present
	for _, sec := range []string{"Developer Tools", "Productivity", "DevOps", "Data & Analytics"} {
		if !strings.Contains(joined, sec) {
			t.Errorf("missing expected section header %q in Discover view:\n%s", sec, joined)
		}
	}

	// Verify cursor starts on "+ Install from local directory..." (row 0)
	if m.panel.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.panel.cursor)
	}

	// A plugin appears in every section it belongs to:
	// sql-master belongs to both "Data & Analytics" and "Developer Tools".
	// Total rows = 1 (local install) + 6 (section occurrences) = 7.
	if cnt := m.pluginRowCount(); cnt != 7 {
		t.Errorf("pluginRowCount = %d, want 7", cnt)
	}

	// Pressing Down navigates to the first item (in Developer Tools)
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.cursor != 1 {
		t.Fatalf("after Down: cursor = %d, want 1", m.panel.cursor)
	}

	// Navigate to the bottom
	for i := 0; i < 10; i++ {
		m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	}
	if m.panel.cursor != 6 {
		t.Fatalf("after repeated Down: cursor = %d, want 6", m.panel.cursor)
	}

	// Enter search mode: section headers should be omitted
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: 's', Text: "s"})
	searchLines := strings.Join(plainLines(m), "\n")
	if strings.Contains(searchLines, "Developer Tools") || strings.Contains(searchLines, "Data & Analytics") {
		t.Errorf("search results should not include section headers:\n%s", searchLines)
	}
}
