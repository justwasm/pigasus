// slash_cycle205_test.go — cycle 205: the action-panel key actions
// (/permissions, /hooks, /agents).
//
// The rows below are the SGR-stripped form of the transcripts captured by
// driving the reference (v1.4.0, 220×50) on the private tmux socket
// `tmux -L agy205`; see docs/cycles/cycle-205/deliverable-action-panels.md.
// Styling that matters (the tab row's bold+underline, the reverse-video
// input cursor) is asserted separately on the raw bytes, because
// stripSGR intentionally erases it.

package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCycle205PermissionsScopeWrap: /permissions stage 1 wraps ↑/↓
// (probe_verify2.py: Up at Project lands on Global).
func TestCycle205PermissionsScopeWrap(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("Up not handled")
	}
	if m.panel.cursor != 2 {
		t.Fatalf("Up at row 0: cursor = %d, want 2 (Global)", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.cursor != 0 {
		t.Fatalf("Down at last row: cursor = %d, want 0 (Project)", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.act.permStage != 1 {
		t.Fatalf("Enter on a scope: stage = %d, want 1", m.panel.act.permStage)
	}
}

// TestCycle205PermissionsTabView asserts the stage-2 frame for a populated
// scope, transcribed from perm-after-enter.raw.txt / permissions-global-stage2.
func TestCycle205PermissionsTabView(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 1)
	m.panel.act.permStage = 1
	m.panel.act.permScope = permScopeShared
	m.panel.act.permTab = 0
	m.panel.act.shared[0] = []string{"command(grep)", "command(ls)"}
	m.panel.cursor = 1
	assertLines(t, plainLines(m), []string{
		"Permissions — Shared with Pigasus",
		" allowlist (2)    denylist (0)    asklist (0)   (←/→ to switch)",
		"",
		"  command(grep)",
		"> command(ls)",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Switch View  a Add rule  e Edit rule  d/⌫ Delete rule  esc Close",
	})
}

// TestCycle205PermissionsEmptyState: the reference prints `No <list> rules.`
// with no leading space (perm-after-enter.raw.txt line 3).
func TestCycle205PermissionsEmptyState(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permStage = 1
	m.panel.act.permScope = permScopeProject
	m.panel.act.permTab = 2
	assertLines(t, plainLines(m), []string{
		"Permissions — Project",
		" allowlist (0)    denylist (0)    asklist (0)   (←/→ to switch)",
		"",
		"No asklist rules.",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Switch View  a Add rule  e Edit rule  d/⌫ Delete rule  esc Close",
	})
}

// TestCycle205PermissionsTabSwitchWraps: ←/→ wrap (0 → 2 → 1 → 0).
func TestCycle205PermissionsTabSwitchWraps(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permStage = 1
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	if m.panel.act.permTab != 2 {
		t.Fatalf("Left from allowlist: tab = %d, want 2", m.panel.act.permTab)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if m.panel.act.permTab != 0 {
		t.Fatalf("Right from asklist: tab = %d, want 0", m.panel.act.permTab)
	}
}

// TestCycle205PermissionsFormRender: the add form's frame. The reference's
// form legend separates the first two legend pairs with a TAB
// (probe_perm_add.py, 3/3 runs); the clone uses two spaces there because its
// cell renderer drops raw control characters — see 存疑 in the cycle report.
func TestCycle205PermissionsFormRender(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permStage = 2
	m.panel.act.permForm = 1
	m.panel.act.permScope = permScopeGlobal
	m.panel.act.permTab = 0
	got := plainLines(m)
	assertLines(t, got, []string{
		"Add Rule — Global — allowlist",
		"",
		"Enter a permission rule:",
		"",
		" ",
		"",
		"Format: action(target)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Close",
	})
	// The input row must carry the reverse-video cursor.
	raw := m.permissionsFormLines()[4]
	if !strings.Contains(raw, "\x1b[7m\x1b[37m \x1b[0m") {
		t.Errorf("input row missing reverse-video cursor: %q", raw)
	}
}

// TestCycle205PermissionsFormError: a rejected rule keeps the form open and
// inserts the error row between the input and the Format hint
// (perm-edit-after.raw.txt).
func TestCycle205PermissionsFormError(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permStage = 2
	m.panel.act.permForm = 2
	m.panel.act.permScope = permScopeGlobal
	m.panel.act.permText = "command(grep)ZZ"
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.act.permStage != 2 {
		t.Fatalf("invalid rule closed the form: stage = %d", m.panel.act.permStage)
	}
	assertLines(t, plainLines(m), []string{
		"Edit Rule — Global — allowlist",
		"",
		"Enter a permission rule:",
		"",
		"command(grep)ZZ ",
		"",
		`✗ invalid grant string: "command(grep)ZZ"`,
		"",
		"Format: action(target)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Close",
	})
}

// TestCycle205ValidateGrant pins the two rejection messages and the
// accepted forms (probe_read.py + probe_actions.py).
func TestCycle205ValidateGrant(t *testing.T) {
	accept := []string{"command(grep)", "command(a|b)", "command()"}
	for _, s := range accept {
		if msg := validateGrant(s); msg != "" {
			t.Errorf("validateGrant(%q) = %q, want accepted", s, msg)
		}
	}
	cases := map[string]string{
		`command(grep)ZZ`: `invalid grant string: "command(grep)ZZ"`,
		`foo(bar)`:        `unknown action "foo" in grant string: "foo(bar)"`,
		`read(*)`:         `unknown action "read" in grant string: "read(*)"`,
		``:                `invalid grant string: ""`,
		`command(`:        `invalid grant string: "command("`,
	}
	for in, want := range cases {
		if got := validateGrant(in); got != want {
			t.Errorf("validateGrant(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCycle205PermissionsAddDelete exercises the add/delete actions on the
// session-local Shared scope so the test never touches settings.json.
func TestCycle205PermissionsAddDelete(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permScope = permScopeShared
	m.panel.act.permStage = 2
	m.panel.act.permForm = 1
	for _, r := range "command(grep)" {
		m.handlePanelKey(tea.Key{Text: string(r)})
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if got := m.panel.act.shared[0]; len(got) != 1 || got[0] != "command(grep)" {
		t.Fatalf("after add: shared[allow] = %q", got)
	}
	if m.panel.act.permStage != 1 {
		t.Fatalf("after add: stage = %d, want 1", m.panel.act.permStage)
	}
	m.panel.cursor = 0
	m.handlePanelKey(tea.Key{Text: "d"})
	if got := m.panel.act.shared[0]; len(got) != 0 {
		t.Fatalf("after delete: shared[allow] = %q, want empty", got)
	}
	// Backspace is the second advertised delete key.
	m.panel.act.shared[0] = []string{"command(ls)"}
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if got := m.panel.act.shared[0]; len(got) != 0 {
		t.Fatalf("after backspace delete: shared[allow] = %q, want empty", got)
	}
}

// TestCycle205PermissionsEditDeletesThenAppends: Edit removes the old rule
// and appends the new one at the end.
func TestCycle205PermissionsEditDeletesThenAppends(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permScope = permScopeShared
	m.panel.act.permStage = 1
	m.panel.act.shared[0] = []string{"command(a)", "command(b)"}
	m.panel.cursor = 0
	m.handlePanelKey(tea.Key{Text: "e"})
	if m.panel.act.permStage != 2 || m.panel.act.permForm != 2 {
		t.Fatalf("e on a row: stage=%d form=%d, want 2/2", m.panel.act.permStage, m.panel.act.permForm)
	}
	if m.panel.act.permText != "command(a)" {
		t.Fatalf("edit prefill = %q, want %q", m.panel.act.permText, "command(a)")
	}
	m.panel.act.permText = "command(c)"
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	got := m.panel.act.shared[0]
	want := []string{"command(b)", "command(c)"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("after edit: %q, want %q", got, want)
	}
	// `e` with no rows selected opens the Add form.
	m.panel.act.shared[0] = nil
	m.panel.cursor = 0
	m.handlePanelKey(tea.Key{Text: "e"})
	if m.panel.act.permForm != 1 {
		t.Fatalf("e with no rows: form = %d, want 1 (add)", m.panel.act.permForm)
	}
}

// TestCycle205HooksMatchersTool: the PreToolUse matcher list
// (hooks-enter.raw.txt).
func TestCycle205HooksMatchersTool(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	m.panel.act.hookStage = 1
	m.panel.act.hookIdx = 0
	assertLines(t, plainLines(m), []string{
		" PreToolUse — Matchers",
		"   Before tool execution",
		"",
		" > + Add new matcher...",
		"   + Match all (no filter)",
		"   No hooks configured",
		"",
		"   ↑/↓ Navigate · enter Select · e Toggle · backspace Delete · esc Back",
	})
}

// TestCycle205HooksMatchersLifecycle: a non-tool hook offers
// "+ Add new hook..." and no "Match all" row (probe_hooks.py).
func TestCycle205HooksMatchersLifecycle(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	m.panel.act.hookStage = 1
	m.panel.act.hookIdx = 2 // PreInvocation
	assertLines(t, plainLines(m), []string{
		" PreInvocation — Matchers",
		"   Before each LLM invocation",
		"",
		" > + Add new hook...",
		"   No hooks configured",
		"",
		"   ↑/↓ Navigate · enter Select · e Toggle · backspace Delete · esc Back",
	})
}

// TestCycle205HooksMatcherForm: the add-matcher form
// (hooks-add-enter.raw.txt).
func TestCycle205HooksMatcherForm(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	m.panel.act.hookStage = 2
	m.panel.act.hookIdx = 0
	assertLines(t, plainLines(m), []string{
		" Add new matcher for PreToolUse",
		"",
		"   Before tool execution",
		"",
		"   Matcher:",
		" >  ",
		"",
		"   Examples:",
		"   • Write (single tool)",
		"   • Write|Edit (multiple tools)",
		"   • Web.* (regex pattern)",
		"",
		"   enter Confirm · esc Cancel",
	})
}

// TestCycle205HooksHookForm: the add-hook form for a lifecycle hook
// (probe_hooks.py, PreInvocation).
func TestCycle205HooksHookForm(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	m.panel.act.hookStage = 2
	m.panel.act.hookIdx = 2
	assertLines(t, plainLines(m), []string{
		" Add new hook",
		"",
		"   Event: PreInvocation — Before each LLM invocation",
		"",
		"   Name:",
		" >  ",
		"",
		"   Command:",
		"      ",
		"",
		"   Timeout (seconds):",
		"     default: 30",
		"",
		"   ⚠ Hooks run shell commands with your full permissions.",
		"",
		"   tab Switch field · enter Confirm · esc Cancel",
	})
}

// TestCycle205HooksNavigation: list → matchers → form → back.
func TestCycle205HooksNavigation(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 1)
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.act.hookStage != 1 || m.panel.act.hookIdx != 1 {
		t.Fatalf("Enter on PostToolUse: stage=%d idx=%d, want 1/1", m.panel.act.hookStage, m.panel.act.hookIdx)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.act.hookStage != 2 {
		t.Fatalf("Enter on the add row: stage=%d, want 2", m.panel.act.hookStage)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.act.hookStage != 1 {
		t.Fatalf("Esc from the form: stage=%d, want 1", m.panel.act.hookStage)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.act.hookStage != 0 || m.panel.kind != panelHooks {
		t.Fatalf("Esc from the matcher list: stage=%d kind=%v", m.panel.act.hookStage, m.panel.kind)
	}
}

// TestCycle205AgentsEnterEcho: Enter on the default agent echoes
// "Already using default agent." and keeps the panel open
// (probe_agents.py).
func TestCycle205AgentsEnterEcho(t *testing.T) {
	m := mkPanel(panelAgents, "/agents", 0)
	m.response = nil
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.kind != panelAgents {
		t.Fatalf("Enter closed the panel: kind = %v", m.panel.kind)
	}
	joined := ""
	for _, l := range m.response {
		joined += stripSGR(l) + "\n"
	}
	if !strings.Contains(joined, "> /agents") {
		t.Errorf("echo missing `> /agents`:\n%s", joined)
	}
	if !strings.Contains(joined, "Already using default agent.") {
		t.Errorf("echo missing the default-agent message:\n%s", joined)
	}
	// Other keys stay inert.
	before := len(m.response)
	m.handlePanelKey(tea.Key{Text: "k"})
	if len(m.response) != before {
		t.Errorf("`k` wrote a response: %q", m.response[before:])
	}
}

// TestCycle205MCPEmptyFrameUnchanged guards the /mcp empty frame against
// regression (cycle 205 deliberately left it as-is).
func TestCycle205MCPEmptyFrameUnchanged(t *testing.T) {
	m := mkPanel(panelMCP, "/mcp", 0)
	assertLines(t, plainLines(m), []string{
		"MCP Servers",
		"",
		"  No MCP servers configured.",
		"",
		"Keyboard: ↑/↓ Navigate  enter Actions  esc Exit",
	})
	if m.panel.kind.exitNotice() {
		t.Error("/mcp must close silently")
	}
}
