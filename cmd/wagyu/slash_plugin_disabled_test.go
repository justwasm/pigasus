// slash_cycle235_test.go — cycle 235: AGY 1.2.16 close-gap.
//
// The reference's /plugin and /mcp panels listed disabled plugins as if
// they were installed, and showed "enabled"/"disabled" without indicating
// the row was also blocked by `disableAllPlugins` or the matching env var.
//
// Cycle 235 adds a third state — [blocked] — that overrides the per-row
// [enabled]/[disabled] badge when the global plugin gate (env var
// AGY_CLI_DISABLE_PLUGINS or config.json's disableAllPlugins boolean) is
// on. The same gate covers /mcp via AGY_CLI_DISABLE_MCP /
// mcp.json's disableAllMCP. The test surface below pins:
//
//   - the badge string for each of the three states,
//   - the precedence rule (env var wins over the on-disk boolean),
//   - the panel rendering and the /mcp listing under the gate.
//
// The cycle-198 Installed-tab tests still pass: the gate is only
// consulted when at least one of the env var / config.json field is
// actually set, so the legacy fallback (no config.json, four stock
// plugins shown as [disabled]) is unchanged.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
)

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
