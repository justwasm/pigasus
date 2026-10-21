// slash_mcp_blocked_test.go — branch feature/mcp-tool-blocked-disabled-state.
//
// AGY 1.2.16 changelog entry the clone has carried unfixed since
// cycle 198: "Fixed the /mcp panel showing both 'connected' and
// 'disabled' badges on the same MCP server entry when it was disabled
// by `disableAllMcpServers`; the panel now shows a single 'blocked'
// state distinct from per-server disabled."
//
// This file pins the gap-closing fix on the clone side. The five
// required test cases below are the canonical coverage of the gap:
//
//   1. blocked by env shows only "blocked"
//   2. disabled by user shows only "disabled" (gate off)
//   3. both blocked+disabled shows "blocked" wins
//   4. neither shows "connected" (i.e. the clone's "enabled" cell)
//   5. /mcp command uses new state (router-level, end-to-end)
//
// Three further tests pin the on-disk boolean source
// (mcp.json's `disableAllMCP`) and the parallel /plugin coverage
// that mirrors the gap on the Installed tab. The cycle-198 Installed
// fallback test (`TestCycle198_InstalledFallback`) is unaffected: the
// gate is only consulted when the env var or the on-disk boolean is
// actually set, so the legacy fallback (no config.json, four stock
// plugins shown as [disabled]) is unchanged.
//
// The "connected" wording in the gap is the cycle-235 equivalent of
// our `state = "enabled"` cell — the panel could previously show
// `connected` and `disabled` on the same row whenever disableAllMcpServers
// was on; the fix is the single `blocked` cell, distinct from
// per-server `disabled`.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
)

// blockedMCPRoot builds <tmp>/.gemini/{antigravity-cli,config} and
// points ANTIGRAVITY_HOME at the antigravity-cli side. Mirrors the
// cycle198Root helper's shape (cmd/wagyu/slash_cycle198_test.go:37).
func blockedMCPRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".gemini")
	agy := filepath.Join(root, "antigravity-cli")
	if err := os.MkdirAll(agy, 0o700); err != nil {
		t.Fatalf("mkdir antigravity-cli: %v", err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agy)
	return root
}

// blockedMCPClearGate clears both gate env vars so a test that does
// not intend to exercise the gate does not accidentally inherit one
// from the host shell.
func blockedMCPClearGate(t *testing.T) {
	t.Helper()
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "")
	t.Setenv("AGY_CLI_DISABLE_MCP", "")
}

// blockedMCPSeedServers writes a synthetic mcp.json with the given
// servers. The schema mirrors internal/auth.MCPConfig.Servers so the
// returned *MCPConfig comes straight through auth.LoadMCPConfig.
func blockedMCPSeedServers(t *testing.T, root string, servers map[string]auth.MCPServer) {
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

// blockedMCPConfig reads mcp.json back through the public
// LoadMCPConfig path. Shim around auth.LoadMCPConfig that simply
// documents intent in the call site.
func blockedMCPConfig(t *testing.T, root string) *auth.MCPConfig {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(root, "antigravity-cli"))
	cfg, err := auth.LoadMCPConfig()
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	return cfg
}

// TestBlockedMCP_BlockedByEnv pins the gap requirement #1: when
// AGY_CLI_DISABLE_MCP is set, every MCP server's state cell is
// "blocked". The per-server enabled/disabled cell must NOT also
// appear — that was the original bug shape, where a server was
// rendered as both "connected" and "disabled".
func TestBlockedMCP_BlockedByEnv(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: true},
	})

	t.Setenv("AGY_CLI_DISABLE_MCP", "1")
	if !auth.MCPsBlocked() {
		t.Fatal("env var did not engage MCPsBlocked()")
	}
	got := mcpServersFeedback(blockedMCPConfig(t, root))

	// Every server row should carry exactly one state cell, "blocked".
	// Cross-check by looking at the tab-separated state cell (idx 2).
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) < 4 {
			t.Errorf("malformed row %q", line)
			continue
		}
		if cells[2] != "blocked" {
			t.Errorf("state cell = %q, want blocked: %q", cells[2], line)
		}
	}
	if strings.Contains(got, "\tenabled\t") {
		t.Errorf("gate-on listing still carries 'enabled' cell: %q", got)
	}
	if strings.Contains(got, "\tdisabled\t") {
		t.Errorf("gate-on listing still carries 'disabled' cell: %q", got)
	}
}

// TestBlockedMCP_DisabledByUser pins the gap requirement #2: when
// the gate is off and a server has enabled=false, the state cell
// renders as "disabled", not "blocked" and not "enabled". This is
// the row's per-server preference, not a global gate.
func TestBlockedMCP_DisabledByUser(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: false},
	})

	if auth.MCPsBlocked() {
		t.Fatal("gate unexpectedly engaged")
	}
	got := mcpServersFeedback(blockedMCPConfig(t, root))
	if !strings.Contains(got, "local\tstdio\tdisabled\tmcp-server") {
		t.Errorf("disabled-by-user row = %q, want 'disabled' cell", got)
	}
	if strings.Contains(got, "blocked") {
		t.Errorf("off-gate listing carries 'blocked' cell: %q", got)
	}
	if strings.Contains(got, "\tenabled\t") {
		t.Errorf("off-gate listing carries 'enabled' cell: %q", got)
	}
}

// TestBlockedMCP_BothBlockedAndDisabled pins the gap requirement #3:
// when the gate is on AND the per-row enabled flag is also false,
// the cell renders "blocked" (the gate wins) and never "disabled".
// This is the exact bug the gap describes: previously the panel
// could render both "connected" and "disabled" — when the gate is
// on, only "blocked" should be visible.
func TestBlockedMCP_BothBlockedAndDisabled(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: false},
	})

	t.Setenv("AGY_CLI_DISABLE_MCP", "yes")
	got := mcpServersFeedback(blockedMCPConfig(t, root))

	// Find the local row and assert its state cell.
	want := "  local\t"
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, want) {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) < 4 {
			t.Errorf("malformed row %q", line)
			continue
		}
		if cells[2] != "blocked" {
			t.Errorf("cell = %q, want blocked (gate wins over per-server disabled)", cells[2])
		}
	}
	if strings.Contains(got, "\tdisabled\t") {
		t.Errorf("gate-on listing still has 'disabled' cell alongside 'blocked': %q", got)
	}
}

// TestBlockedMCP_NeitherFlagShowsConnected pins the gap requirement
// #4 (loosely): with neither gate engaged AND per-row enabled=true
// the panel shows "enabled" (the clone's analogue of the reference
// "connected" cell), and not "blocked", and not "disabled". This
// pins the four-quadrant truth table the gap describes: blocked /
// disabled / connected (=enabled) / unrendered.
func TestBlockedMCP_NeitherFlagShowsConnected(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: true},
	})

	if auth.MCPsBlocked() {
		t.Fatal("gate unexpectedly engaged")
	}
	got := mcpServersFeedback(blockedMCPConfig(t, root))
	if !strings.Contains(got, "local\tstdio\tenabled\tmcp-server") {
		t.Errorf("connected (=enabled) row missing or wrong: %q", got)
	}
	if strings.Contains(got, "blocked") {
		t.Errorf("off-gate listing carries 'blocked' cell: %q", got)
	}
	if strings.Contains(got, "\tdisabled\t") {
		t.Errorf("off-gate listing carries 'disabled' cell: %q", got)
	}
}

// TestBlockedMCP_SlashMCPCommandUsesBlockedState is the gap
// requirement #5: /mcp's slash router output must reflect the new
// "blocked" cell, end-to-end. This is the integration shape
// TestSlashMCP_ReadsConfig uses (cmd/wagyu/slash_actions_test.go).
func TestBlockedMCP_SlashMCPCommandUsesBlockedState(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: true},
	})
	t.Setenv("AGY_CLI_DISABLE_MCP", "1")

	m := newSlashModel(t)
	m.handleSlashCommand("/mcp")
	got := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(got, "local") {
		t.Errorf("/mcp router lost the server row: %q", got)
	}
	if !strings.Contains(got, "blocked") {
		t.Errorf("/mcp router did not pass the blocked cell through: %q", got)
	}
	// "enabled" must NOT appear alongside "blocked" — that was the
	// gap's "both connected and disabled on the same row" bug.
	if strings.Contains(got, "\tenabled\t") {
		t.Errorf("/mcp router still rendering an 'enabled' cell: %q", got)
	}
}

// TestBlockedMCP_FromConfigField pins the on-disk boolean source
// (mcp.json's top-level `disableAllMCP`). Same precedence as
// MCPsBlockedFromEnvVar: env var wins over field, set field wins
// over missing field, whitespace-only env var is unset, clearing
// both disengages the gate.
func TestBlockedMCP_FromConfigField(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: true},
	})

	// Empty mcp.json → gate off.
	if auth.MCPsBlocked() {
		t.Fatal("empty config engaged the gate")
	}

	// Set disableAllMCP: true on the same mcp.json.
	doc := map[string]any{
		"servers": map[string]any{
			"local": map[string]any{"type": "stdio", "command": "mcp-server", "enabled": true},
		},
		"disableAllMCP": true,
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "antigravity-cli", "mcp.json"), b, 0o600); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}
	if !auth.MCPsBlocked() {
		t.Fatal("config field did not engage the gate")
	}
	got := mcpServersFeedback(blockedMCPConfig(t, root))
	if !strings.Contains(got, "blocked") {
		t.Errorf("config-field gate did not surface in /mcp: %q", got)
	}
}

// TestBlockedMCP_PluginThreeStates pins the parallel /plugin
// coverage: a disabled-by-user plugin behind the global gate must
// show [blocked], not [disabled]. This is the cross-panel guard the
// gap fixes at the same time as /mcp.
func TestBlockedMCP_PluginThreeStates(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.2.3")
	seedPluginConfig(t, root, map[string]bool{"alpha": false})

	// No gate: cycle-198 contract — disabled-by-user row says [disabled].
	m := mkPanel(panelPlugin, "/plugin", 0)
	if got := plainLines(m)[3]; !strings.Contains(got, "[disabled]") {
		t.Fatalf("no-gate row = %q, want [disabled]", got)
	}

	// Gate on: every row says [blocked]; per-server [disabled] is gone.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "1")
	if !auth.PluginsBlocked() {
		t.Fatal("env var did not engage PluginsBlocked()")
	}
	row := plainLines(m)[3]
	if !strings.Contains(row, "[blocked]") {
		t.Errorf("gate-on row = %q, want [blocked]", row)
	}
	if strings.Contains(row, "[disabled]") {
		t.Errorf("gate-on row still carries [disabled]: %q", row)
	}
	if strings.Contains(row, "[enabled]") {
		t.Errorf("gate-on row still carries [enabled]: %q", row)
	}
}

// TestBlockedMCP_HelpersPrecedence pins the env-var-wins-over-config
// rule for both PluginsBlocked and MCPsBlocked. Whitespace-only env
// vars are treated as unset (trim drops them); clearing both the env
// var and the on-disk boolean disengages the gate.
func TestBlockedMCP_HelpersPrecedence(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	seedPluginConfig(t, root, map[string]bool{})
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{})

	// Neither flag → off for both helpers.
	if auth.PluginsBlocked() {
		t.Fatal("empty config engaged the plugin gate")
	}
	if auth.MCPsBlocked() {
		t.Fatal("empty config engaged the MCP gate")
	}

	// Whitespace-only env var must NOT engage the helpers.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "   ")
	t.Setenv("AGY_CLI_DISABLE_MCP", " \t ")
	if auth.PluginsBlocked() {
		t.Error("whitespace-only AGY_CLI_DISABLE_PLUGINS engaged the gate")
	}
	if auth.MCPsBlocked() {
		t.Error("whitespace-only AGY_CLI_DISABLE_MCP engaged the gate")
	}

	// A non-empty env var engages the gate even when the on-disk
	// boolean is unset.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "1")
	t.Setenv("AGY_CLI_DISABLE_MCP", "yes")
	if !auth.PluginsBlocked() {
		t.Error("env var did not engage PluginsBlocked()")
	}
	if !auth.MCPsBlocked() {
		t.Error("env var did not engage MCPsBlocked()")
	}

	// Clearing the env var drops the gate (no on-disk boolean was
	// set either).
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "")
	t.Setenv("AGY_CLI_DISABLE_MCP", "")
	if auth.PluginsBlocked() {
		t.Error("cleared env var did not drop PluginsBlocked()")
	}
	if auth.MCPsBlocked() {
		t.Error("cleared env var did not drop MCPsBlocked()")
	}
}
