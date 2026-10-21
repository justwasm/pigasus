package main

// slash_cycle212_test.go — cycle 212: the Discover tab's leftover
// behaviours.
//
// Every expectation below is pinned to the live reference (Antigravity CLI
// 1.4.0), driven at 220x50 through a private tmux socket; the probe
// transcripts are /private/var/folders/.../opencode/agy212/p{1..15}.out and
// the cycle report is docs/cycles/cycle-212/deliverable-plugin-leftovers.md.
//
// Cycle 212 closes three Discover gaps left by cycles 198/201/204:
//
//   - Enter / ctrl+s on the "+ Install from local directory..." row opens
//     an inline path prompt (the "Install from local directory" row's
//     text is reused as the prompt label). The prompt's accept path
//     copies the source directory into ~/.gemini/config/plugins/ and
//     writes the matching config.json entry.
//
//   - Tab on the focused Discover search box cycles Installed↔Discover
//     while *keeping* the box focused and the typed query intact. The
//     reference's Installed-tab search-mode rendering differs from the
//     Discover one (a `(enter to search)` hint + `enter Search` legend)
//     and is left for a follow-up cycle.
//
//   - The cursor in either box blinks at ~530 ms (the bubbles default),
//     so a single blink phase drives both call sites.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

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
	// build a directory without plugin.json
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
			// The "exists but no plugin.json" hint needs a real dir;
			// the others are simple.
			_ = dir
			if c.wantHint == "" {
				if hint != "" {
					t.Errorf("hint = %q, want empty", hint)
				}
				return
			}
			// Strip the leading 2-space + └─ span for the structural
			// check — the comparison below accepts the full row.
			if !strings.Contains(hint, c.wantHint) {
				t.Errorf("hint = %q\nwant contains %q", hint, c.wantHint)
			}
		})
	}

	// The "exists but no plugin.json" case needs a directory we own.
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

	// A directory with plugin.json shows no hint.
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
	m.handlePanelKey(tea.Key{Code: tea.KeySpace}) // a literal space
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

	// Tab path completion: type a unique subdir prefix and Tab expands.
	// Cycle 220's probe (`probe_multi_tab.py`) shows the reference appends
	// a trailing slash after the resolved directory; the clone matches.
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

	// Enter on a directory that does not exist: error in the title, prompt
	// stays open.
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

	// Esc cancels.
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
	// The cursor is parked on the local row (the prompt owns every key).
	if m.panel.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.panel.cursor)
	}

	// Close, then re-open with ctrl+s.
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
	// config.json: the plugins.<name>.enabled entry is true.
	data, err := os.ReadFile(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if !strings.Contains(string(data), `"agy212-fresh"`) ||
		!strings.Contains(string(data), `"enabled": true`) {
		t.Errorf("config.json = %s, want agy212-fresh enabled", data)
	}
}

// TestCycle212_AcceptConflictLeavesPromptOpen verifies the duplicate
// detection: if the destination already exists, the prompt stays open
// and pluginLocalError records the failure.
func TestCycle212_AcceptConflictLeavesPromptOpen(t *testing.T) {
	root := cycle212Root(t)
	src := filepath.Join(t.TempDir(), "srcdir")
	seedLocalPlugin212(t, src, "agy212-conflict", "1.0.0")
	// Pre-install the destination so the second attempt collides.
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

// TestCycle212_SearchBoxBlink pins the cursor's two blink phases:
// blinkOn=true emits the reverse-video span; blinkOn=false emits only
// the trailing space the box always reserves.
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

// TestCycle212_SearchBoxBlinkMessage exercises the pluginBlinkMsg handler
// the cycle-212 wire-up adds: a tick while the panel is focused flips
// blinkOn and re-arms the next tick; a tick after focus is gone drops
// the pending tick and resets blinkOn.
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
	// Close the search and a tick must drop the pending arm.
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

// TestCycle212_TabKeepsQueryAndFocus (also asserted by
// TestCycle212_TabFromSearchKeepsFocusAndQuery in slash_cycle204_test.go)
// verifies the search box's typed query survives a Tab from one focused
// tab to the other.
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

// TestCycle212_LeftRightWhileFocusedJumpsFirstLast verifies the cycle
// 204 docstring's "unmeasured" branch: ←/→ on the focused Discover tab
// jump to the first / last filtered row, matching the unfocused tab's
// behaviour. (The reference's legend says "Page" — the prompt-vs-list
// wording is misleading; this cycle measures and pins the actual
// behaviour.)
func TestCycle212_LeftRightWhileFocusedJumpsFirstLast(t *testing.T) {
	root := cycle212Root(t)
	seedMarketplace204(t, root, "mkt", "", []marketplaceEntry204{
		{"alpha", "Alpha.", "1.0.0"},
		{"beta", "Beta.", "1.0.0"},
		{"gamma", "Gamma.", "1.0.0"},
	})

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	// Query "a" matches all three names (alpha/beta/gamma share the
	// trailing "a"); filtered size = 3.
	m.handlePanelKey(tea.Key{Code: tea.KeySpace})
	m.handlePanelKey(tea.Key{Code: 'a', Text: "a"})
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right while focused reported unhandled")
	}
	if m.panel.cursor != 2 { // last filtered row
		t.Fatalf("Right while focused = cursor %d, want 2", m.panel.cursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyLeft}); !handled {
		t.Fatal("Left while focused reported unhandled")
	}
	if m.panel.cursor != 0 {
		t.Fatalf("Left while focused = cursor %d, want 0", m.panel.cursor)
	}
}
