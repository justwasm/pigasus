package main

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
