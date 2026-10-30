// slash_plugin_shelf_test.go — cycle 364 (agy 1.4.11): the /plugin Discover
// tab's shelves.
//
// 【一手】 agy 1.4.11: "each shelf now shows its first 6 plugins followed by a
// `⋯ N more — enter to show` row, `Ctrl+E` expands or collapses the shelf
// under the cursor, and `[` and `]` jump to the previous or next shelf."
package cliapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/pluginsui"
)

// cycle364Entry is one marketplace entry with a category, so the test can
// steer the Discover tab's section grouping.
type cycle364Entry struct {
	name     string
	desc     string
	category string
}

// cycle364Root seeds an Antigravity home with an empty marketplace cache.
func cycle364Root(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".gemini")
	agy := filepath.Join(root, "antigravity-cli")
	if err := os.MkdirAll(agy, 0700); err != nil {
		t.Fatalf("mkdir antigravity-cli: %v", err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agy)
	return root
}

// seedCycle364Marketplace writes cache/marketplaces/<mkt>/marketplace.json.
func seedCycle364Marketplace(t *testing.T, root, mkt string, entries []cycle364Entry) {
	t.Helper()
	dir := filepath.Join(root, "antigravity-cli", "cache", "marketplaces", mkt)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir marketplace: %v", err)
	}
	plugins := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		plugins = append(plugins, map[string]any{
			"name": e.name, "description": e.desc, "version": "1.0.0",
			"category": e.category,
		})
	}
	writeJSONFile(t, filepath.Join(dir, "marketplace.json"), map[string]any{
		"name": mkt, "description": "", "plugins": plugins,
	})
}

// cycle364Discover seeds a feed of eight plugins that land in no section of
// their own (they fall back to Developer Tools, so the panel shows them as
// one unnamed shelf) and opens the Discover tab.
func cycle364Discover(t *testing.T) *runMode {
	t.Helper()
	root := cycle364Root(t)
	entries := make([]cycle364Entry, 0, 8)
	for _, name := range []string{
		"alpha", "bravo", "cupid", "delta", "ember", "fox", "gamma", "hotel",
	} {
		entries = append(entries, cycle364Entry{name: name, desc: name + " plugin."})
	}
	seedCycle364Marketplace(t, root, "mkt", entries)
	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	return m
}

// TestCycle364_ShelfShowsSixThenMoreRow pins the shelf's shape: the first
// six plugins, then the "⋯ N more — enter to show" row.
func TestCycle364_ShelfShowsSixThenMoreRow(t *testing.T) {
	m := cycle364Discover(t)
	lines := plainLines(m)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "⋯ 2 more — enter to show") {
		t.Fatalf("the shelf does not carry the more row:\n%s", joined)
	}
	// Six plugins, the more row, and the local-install row.
	if got := m.pluginRowCount(); got != 8 {
		t.Errorf("pluginRowCount = %d, want 8 (6 plugins + more row + local row)", got)
	}
	// The seventh plugin is behind the more row.
	if strings.Contains(joined, "gamma") {
		t.Errorf("the seventh plugin is visible although the shelf is collapsed:\n%s", joined)
	}
}

// TestCycle364_CtrlEExpandsAndCollapsesTheShelf pins ctrl+e.
func TestCycle364_CtrlEExpandsAndCollapsesTheShelf(t *testing.T) {
	m := cycle364Discover(t)
	m.panel.cursor = 1 // the first plugin of the shelf
	m.handlePluginKey(tea.Key{Code: 'e', Mod: tea.ModCtrl})

	lines := plainLines(m)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "more — enter to show") {
		t.Errorf("the expanded shelf still carries the more row:\n%s", joined)
	}
	if !strings.Contains(joined, "gamma") {
		t.Errorf("the expanded shelf does not show its tail:\n%s", joined)
	}
	if got := m.pluginRowCount(); got != 9 {
		t.Errorf("pluginRowCount = %d, want 9 (8 plugins + local row)", got)
	}

	// ctrl+e again collapses it.
	m.handlePluginKey(tea.Key{Code: 'e', Mod: tea.ModCtrl})
	if !strings.Contains(strings.Join(plainLines(m), "\n"), "⋯ 2 more — enter to show") {
		t.Errorf("the collapsed shelf lost its more row:\n%s", strings.Join(plainLines(m), "\n"))
	}
}

// TestCycle364_EnterOnTheMoreRowShowsTheShelf pins the row's own hint:
// "enter to show".
func TestCycle364_EnterOnTheMoreRowShowsTheShelf(t *testing.T) {
	m := cycle364Discover(t)
	// Row 0 is the local-install row, rows 1..6 the plugins, row 7 the
	// more row.
	m.panel.cursor = 7
	m.handlePluginKey(tea.Key{Code: tea.KeyEnter})
	joined := strings.Join(plainLines(m), "\n")
	if strings.Contains(joined, "more — enter to show") {
		t.Errorf("enter on the more row did not show the shelf:\n%s", joined)
	}
	if !strings.Contains(joined, "gamma") {
		t.Errorf("enter on the more row did not show the shelf's tail:\n%s", joined)
	}
}

// TestCycle364_ShelfJumpKeys pins `[` and `]`.
func TestCycle364_ShelfJumpKeys(t *testing.T) {
	root := cycle364Root(t)
	seedCycle364Marketplace(t, root, "mkt", []cycle364Entry{
		{name: "opsbeta", desc: "Ops.", category: "devops"},
		{name: "dbalpha", desc: "Data.", category: "databases"},
	})
	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover

	shelves := m.pluginDiscoverShelves(pluginMarketplaces())
	if len(shelves) != 2 {
		t.Fatalf("shelves = %d, want 2", len(shelves))
	}
	// Row 0 is the local-install row; row 1 is the first shelf's plugin.
	m.panel.cursor = 1
	m.handlePluginKey(tea.Key{Code: ']'})
	if m.panel.cursor != 2 {
		t.Errorf("] left the cursor at %d, want 2 (the next shelf's first row)", m.panel.cursor)
	}
	m.handlePluginKey(tea.Key{Code: '['})
	if m.panel.cursor != 1 {
		t.Errorf("[ left the cursor at %d, want 1 (the previous shelf's first row)", m.panel.cursor)
	}
	// The jump wraps around at either end.
	m.handlePluginKey(tea.Key{Code: '['})
	if m.panel.cursor != 2 {
		t.Errorf("[ did not wrap: cursor at %d, want 2", m.panel.cursor)
	}
	m.handlePluginKey(tea.Key{Code: ']'})
	if m.panel.cursor != 1 {
		t.Errorf("] did not wrap: cursor at %d, want 1", m.panel.cursor)
	}
}

// TestCycle364_LegendShowsTheShelfKeys pins the footer's shelf hints.
func TestCycle364_LegendShowsTheShelfKeys(t *testing.T) {
	m := cycle364Discover(t)
	legend := m.pluginDiscoverLegend(false)
	plain := stripSGR(legend)
	if !strings.Contains(plain, "Expand Shelf") {
		t.Errorf("the legend misses the expand hint:\n%s", plain)
	}
	if !strings.Contains(plain, "Prev/Next Shelf") {
		t.Errorf("the legend misses the shelf-jump hint:\n%s", plain)
	}
	m.panel.cursor = 1
	m.handlePluginKey(tea.Key{Code: 'e', Mod: tea.ModCtrl})
	if plain := stripSGR(m.pluginDiscoverLegend(false)); !strings.Contains(plain, "Collapse Shelf") {
		t.Errorf("the legend misses the collapse hint:\n%s", plain)
	}
}

// TestCycle364_ShelfLimitIsSix pins the limit itself.
func TestCycle364_ShelfLimitIsSix(t *testing.T) {
	if pluginsui.ShelfLimit != 6 {
		t.Fatalf("ShelfLimit = %d, want 6", pluginsui.ShelfLimit)
	}
}
