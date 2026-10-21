package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/internal/onboarding"
)

func TestConfigSearchFiltering(t *testing.T) {
	all := configRows()
	if len(configRowsFiltered("")) != len(all) {
		t.Fatalf("empty query should return all %d rows, got %d", len(all), len(configRowsFiltered("")))
	}

	// Name filter
	colorRows := configRowsFiltered("color")
	if len(colorRows) != 1 || colorRows[0].name != "Color Scheme" {
		t.Fatalf("expected 1 row for 'color' (Color Scheme), got %d: %+v", len(colorRows), colorRows)
	}

	// Description filter
	descRows := configRowsFiltered("system clipboard")
	if len(descRows) != 1 || descRows[0].name != "Copy on Select" {
		t.Fatalf("expected 'Copy on Select' for 'system clipboard', got %d: %+v", len(descRows), descRows)
	}

	// Option display value filter
	optRows := configRowsFiltered("accept-edits")
	found := false
	for _, r := range optRows {
		if r.name == "Agent Mode" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'Agent Mode' to match 'accept-edits', got %+v", optRows)
	}

	// No match
	none := configRowsFiltered("nonexistent_random_setting_xyz")
	if len(none) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(none))
	}
}

func TestConfigSearchKeyHandling(t *testing.T) {
	m := &runMode{
		panel: slashPanel{
			kind: panelConfig,
			cmd:  "/settings",
		},
		width:  120,
		height: 40,
	}

	// Type 'c'
	m.handlePanelKey(tea.Key{Code: 'c', Text: "c"})
	if m.panel.configQuery != "c" {
		t.Fatalf("expected configQuery 'c', got %q", m.panel.configQuery)
	}

	// Type 'o'
	m.handlePanelKey(tea.Key{Code: 'o', Text: "o"})
	if m.panel.configQuery != "co" {
		t.Fatalf("expected configQuery 'co', got %q", m.panel.configQuery)
	}

	// Check filtered view
	lines := m.panelConfigLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Search: co") {
		t.Fatalf("expected search line with 'co', got:\n%s", joined)
	}

	// Backspace
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.configQuery != "c" {
		t.Fatalf("expected configQuery 'c' after backspace, got %q", m.panel.configQuery)
	}

	// Esc once: clears search query, panel stays open
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.configQuery != "" {
		t.Fatalf("expected empty configQuery after first Esc, got %q", m.panel.configQuery)
	}
	if m.panel.kind != panelConfig {
		t.Fatalf("expected panel to remain open after clearing search, got %v", m.panel.kind)
	}

	// Esc second time: closes panel
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.kind != panelNone {
		t.Fatalf("expected panel to close after second Esc, got %v", m.panel.kind)
	}
}

func TestConfigSubmenuEnterAndSave(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := &runMode{
		panel: slashPanel{
			kind: panelConfig,
			cmd:  "/settings",
		},
		width:  120,
		height: 40,
	}

	// Find index of Agent Mode (usually 0)
	rows := configRowsFiltered("")
	agentIdx := -1
	for i, r := range rows {
		if r.name == "Agent Mode" {
			agentIdx = i
			break
		}
	}
	if agentIdx < 0 {
		t.Fatal("Agent Mode row not found")
	}

	m.panel.cursor = agentIdx
	// Press Enter to open submenu
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.configSubmenu {
		t.Fatal("expected configSubmenu to be true after Enter")
	}

	lines := m.panelConfigLines()
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "enter Save") || !strings.Contains(joined, "esc Cancel") {
		t.Fatalf("expected submenu keyboard hint, got:\n%s", joined)
	}

	// Down arrow to change option
	prevCursor := m.panel.configSubmenuCursor
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.configSubmenuCursor != prevCursor+1 && m.panel.configSubmenuCursor != prevCursor {
		t.Fatalf("expected cursor to move, was %d now %d", prevCursor, m.panel.configSubmenuCursor)
	}

	// Esc to cancel without saving
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.configSubmenu {
		t.Fatal("expected configSubmenu to be false after Esc")
	}

	// Open again, press Enter to save
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.configSubmenu {
		t.Fatal("expected submenu to open again")
	}
	m.panel.configSubmenuCursor = 1 // 'accept-edits'
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.configSubmenu {
		t.Fatal("expected submenu to close after save Enter")
	}

	val, err := auth.SettingsField("agentMode")
	if err != nil || val != "accept-edits" {
		t.Fatalf("expected agentMode 'accept-edits' saved in settings, got %v (err=%v)", val, err)
	}
}

func TestColorSchemePreviewCardRendering(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := &runMode{
		panel: slashPanel{
			kind:        panelConfig,
			cmd:         "/settings",
			configQuery: "color",
		},
		width:  120,
		height: 40,
	}

	rows := configRowsFiltered(m.panel.configQuery)
	if len(rows) != 1 || rows[0].name != "Color Scheme" {
		t.Fatalf("expected 1 row 'Color Scheme', got %+v", rows)
	}

	m.panel.cursor = 0
	// Open Color Scheme submenu
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.configSubmenu {
		t.Fatal("expected configSubmenu = true")
	}

	lines := m.panelConfigLines()
	rawJoined := strings.Join(lines, "\n")
	joined := stripANSI(rawJoined)

	// Verify right-side preview box border glyphs
	if !strings.Contains(joined, "╭─────────────────────────────────────────────────────────────╮") {
		t.Fatalf("expected top border of preview box in:\n%s", joined)
	}
	if !strings.Contains(joined, "╰─────────────────────────────────────────────────────────────╯") {
		t.Fatalf("expected bottom border of preview box in:\n%s", joined)
	}

	// Verify sample code snippet contents
	if !strings.Contains(joined, "func  greet(name") && !strings.Contains(joined, "func greet(name") {
		t.Fatalf("expected greet function in preview box in:\n%s", joined)
	}
	if !strings.Contains(joined, "▾ Thought Process") {
		t.Fatalf("expected Thought Process in preview box in:\n%s", joined)
	}
	if !strings.Contains(joined, "⚙ tool:") || !strings.Contains(joined, "◉ task:") || !strings.Contains(joined, "✗ error:") {
		t.Fatalf("expected tool, task, error glyphs in preview box in:\n%s", joined)
	}

	// Verify left-side options
	if !strings.Contains(joined, "terminal") || !strings.Contains(joined, "tokyo night") {
		t.Fatalf("expected terminal and tokyo night schemes listed on left side")
	}

	// Verify live preview update when cursor moves to Tokyo Night (index 7)
	m.panel.configSubmenuCursor = 7
	linesTokyo := m.panelConfigLines()
	joinedTokyo := strings.Join(linesTokyo, "\n")
	pTokyo := onboarding.PaletteFor(onboarding.SchemeTokyoNight)
	tokyoCodeStr := fmt.Sprintf("\x1b[38;5;%dm", pTokyo.Keyword)
	if !strings.Contains(joinedTokyo, tokyoCodeStr) {
		t.Fatalf("expected Tokyo Night keyword color %s in preview output", tokyoCodeStr)
	}

	// Save Tokyo Night
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.configSubmenu {
		t.Fatal("expected submenu closed after save")
	}

	saved, err := auth.SettingsField("colorScheme")
	if err != nil || saved != "tokyo night" {
		t.Fatalf("expected colorScheme 'tokyo night' saved, got %v (err=%v)", saved, err)
	}
}

func TestAllMenuItemsCanExpandWithEnter(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := &runMode{
		panel: slashPanel{
			kind: panelConfig,
			cmd:  "/settings",
		},
		width:  120,
		height: 40,
	}

	rows := configRows()
	if len(rows) < 18 {
		t.Fatalf("expected at least 18 settings rows, got %d", len(rows))
	}

	for i, r := range rows {
		m.panel.cursor = i
		m.panel.configSubmenu = false

		// Press Enter to expand
		m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
		if !m.panel.configSubmenu {
			t.Fatalf("row %d (%q): expected configSubmenu to open on Enter", i, r.name)
		}

		lines := m.panelConfigLines()
		joined := stripANSI(strings.Join(lines, "\n"))
		if !strings.Contains(joined, r.name) {
			t.Fatalf("row %d (%q): expected name in expanded output:\n%s", i, r.name, joined)
		}
		if !strings.Contains(joined, "(current)") {
			t.Fatalf("row %d (%q): expected (current) marker in expanded options:\n%s", i, r.name, joined)
		}

		// Press Esc to collapse
		m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
		if m.panel.configSubmenu {
			t.Fatalf("row %d (%q): expected configSubmenu to close on Esc", i, r.name)
		}
	}
}

func TestColorSchemeLiveThemeUpdate(t *testing.T) {
	cycle196TestHome(t, map[string]any{"colorScheme": "terminal"})
	t.Cleanup(func() {
		mdrender.SetColorScheme("solarized light")
	})

	m := &runMode{
		panel: slashPanel{
			kind:        panelConfig,
			cmd:         "/settings",
			configQuery: "color",
		},
		width:  120,
		height: 40,
	}

	rows := configRowsFiltered(m.panel.configQuery)
	if len(rows) != 1 || rows[0].name != "Color Scheme" {
		t.Fatalf("expected 1 row 'Color Scheme', got %+v", rows)
	}

	// 1. Open submenu
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.configSubmenu {
		t.Fatal("expected submenu open")
	}

	// 2. Move down to "light" (index 1)
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if mdrender.CurrentColorScheme() != "light" {
		t.Fatalf("expected mdrender live theme 'light', got %q", mdrender.CurrentColorScheme())
	}

	// 3. Move down to "solarized light" (index 2)
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if mdrender.CurrentColorScheme() != "solarized light" {
		t.Fatalf("expected mdrender live theme 'solarized light', got %q", mdrender.CurrentColorScheme())
	}

	// 4. Cancel with Esc: should revert live theme to original "terminal"
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.configSubmenu {
		t.Fatal("expected submenu closed")
	}
	if mdrender.CurrentColorScheme() != "terminal" {
		t.Fatalf("expected mdrender live theme reverted to 'terminal', got %q", mdrender.CurrentColorScheme())
	}

	// 5. ←/→ on main list should cycle live and persist
	handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if !handled || cmd == nil {
		t.Fatalf("expected KeyRight to return handled=true and clearScreen cmd")
	}
	if mdrender.CurrentColorScheme() != "light" {
		t.Fatalf("expected mdrender live theme 'light' after →, got %q", mdrender.CurrentColorScheme())
	}
	saved, _ := auth.SettingsField("colorScheme")
	if saved != "light" {
		t.Fatalf("expected colorScheme 'light' persisted, got %v", saved)
	}
}
