// slash_psif_test.go — cycle psif: the /plugin Installed tab's
// search-input state machine.
//
// AGY 1.3.3 changelog line 46:
//
//	"Improved search in the `/plugin` Installed tab: you can now type
//	 spaces in a search query, press `Enter` to finish searching, then
//	 use `Space` to toggle the selected plugin and `Enter` to expand its
//	 details; start typing again to resume the search."
//
// The cycle pins the four states the changelog describes and the three
// transitions between them:
//
//	Typing       typing space / printable keys append to the query;
//	             Enter commits → Committed.
//	Committed    Space toggles the highlighted row's enabled state;
//	             Enter toggles the highlighted row's detail block;
//	             typing any printable key appends to the query and
//	             narrows the filter live (the user is back in
//	             Typing-without-resetting-the-filter). Esc leaves
//	             search; the panel-row actions revert to the
//	             unfocused Space / Enter semantics.
//
// The cycle also pins a latent cursor/list bug that was hidden while
// the only test fixtures seeded a single matching plugin: the cycle-
// 219 `togglePluginExpanded` (and the unfocused `togglePluginEnabled`)
// read `installedPluginRows()` unconditionally, so the cursor index
// always pointed at the *full* list — which agreed with the filtered
// list only when they were the same set. With more than one matching
// plugin the cursor and the action would disagree. The helper
// `installedActiveRows` is the shared fix; both call sites pick it up.
//
// See docs/cycles/plugin-search-input-flow/deliverable.md for the
// cycle write-up and the AGY 1.3.3 changelog line that drove it.
package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
)

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
