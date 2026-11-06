// slash_plugin_tabs_test.go — the /plugin command's own dispatch and the
// MCP gate.
//
// Cycle 381 moved the /plugin *panel* into internal/ui/views/pluginsui and
// the agents loader into internal/features/plugins/core. What stays here is
// the host's own subject: the slash-command dispatch, and the MCP gate that
// the /mcp panel reads.

package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
)

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
			if m.overlays.panel.kind != panelregistry.None {
				t.Errorf("handleSlashCommand(%q) opened panel kind %d; want panelregistry.None", tc.command, m.overlays.panel.kind)
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
			if m.overlays.panel.kind != panelregistry.None {
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
	if m.overlays.panel.kind != panelregistry.None {
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
// The cycle-198 Badge(true) / Badge(false) byte-strings are
// unchanged — only pluginRowBadge (the helper the panel actually calls)
// grows the third state.

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

// TestPluginBlinkTick_ArmsWhileFocused pins the host's tick handler: it
// re-arms the blink while a cursor is on screen and it stops when the
// focus is gone.
func TestPluginBlinkTick_ArmsWhileFocused(t *testing.T) {
	m := mkPanel(panelregistry.Plugin, "/plugin", 0)
	p := m.pluginPanel()
	p.HandleKey(tea.Key{Code: tea.KeyTab})   // the Discover tab
	p.HandleKey(tea.Key{Code: tea.KeySpace}) // the search box takes the focus
	if !p.Blinking() {
		t.Fatal("the focused search box is not reported as blinking")
	}
	if _, cmd := m.Update(pluginsui.BlinkMsg{}); cmd == nil {
		t.Fatal("the tick did not re-arm the blink while the cursor was on screen")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc}) // the focus is gone
	if _, cmd := m.Update(pluginsui.BlinkMsg{}); cmd != nil {
		t.Fatal("the tick armed another blink after the focus was gone")
	}
}

// TestDPL_PanelCloseResetsFilter: the structural fix the AGY 1.2.7
// closure landed. The "enabled only" filter lives in the panel's own
// state, and the host rebuilds the panel every time it opens, so a
// closed panel's filter never survives into the next session.
//
// The bug the closure fixed was a filter held in a session-level cache:
// closePanel did not reach it, so the disabled rows stayed gone. The
// filter is the panel's now, and the panel is rebuilt on every open.
func TestDPL_PanelCloseResetsFilter(t *testing.T) {
	m := mkPanel(panelregistry.Plugin, "/plugin", 0)
	p := m.pluginPanel()
	p.HandleKey(tea.Key{Code: '/'}) // flip the customization filter on
	if !p.EnabledOnly() {
		t.Fatal("/ did not flip the enabled-only filter on")
	}
	// closePanel wipes the panel struct.
	m.closePanel()
	// Re-opening the panel builds a fresh one.
	m.overlays.panel = slashPanel{kind: panelregistry.Plugin, cmd: "/plugin"}
	m.initOpenedSlashPanelData(panelregistry.Plugin)
	if m.pluginPanel().EnabledOnly() {
		t.Error("the reopened panel kept the filter — AGY 1.2.7 bug regressed")
	}
}
