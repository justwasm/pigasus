package tui

// slash_yolo.go — /yolo, the mid-session equivalent of
// --dangerously-skip-permissions.
//
// The flag is read at startup in two independent places:
//
//  1. the permission policy (setup.PolicyFrom → Policy.SkipPermissions),
//     which makes Policy.DecideCall return Allow outright, and
//  2. the terminal sandbox (setup.SandboxFrom → Sandbox.Enabled), the
//     second gate SandboxBoundaryMiddleware checks *before* the policy
//     ever runs.
//
// Turning only the first one off leaves run_command dying at the
// sandbox, so a mid-session switch has to reach both. They are not
// interchangeable: the policy says "do not ask", the sandbox says "you
// may not leave the workspace at all".
//
// # Why the sandbox needs a rebuild
//
// The policy is shared: m.session.policy is one pointer that the
// parent's runner, the subagent gate and the /config panel all read,
// so flipping SkipPermissions on it is visible everywhere at once.
//
// The sandbox is not shared. It is a struct copied by value into three
// places at build time — permissionRunner.sandbox (read per tool call
// by SandboxBoundaryMiddleware), subagentPermissionGate.sandbox (read
// per Decide) and the tool declarations (AdvertisedToolDeclsWithSandbox
// decides whether run_command advertises the bypass-sandbox parameter).
// So the switch writes m.tools.sandbox and rebuilds the tool surface
// through reloadPluginsAndMCP, which re-reads m.tools.sandbox.Enabled
// and rebuilds both the decls and the runner. Every call after that
// sees the new state.
//
// A turn already in flight keeps the runner it captured when it
// started (agentturn.Env.ToolRunner), so the switch applies from the
// next turn on — the same granularity as the flag itself, which is
// read when a run starts.
//
// # Restoring without a second copy of the rule
//
// /yolo off does not guess the sandbox from the flag set: the startup
// value is decided by setup.SandboxFrom, which combines settings.json's
// enableTerminalSandbox with --dangerously-skip-permissions. A second
// copy of that rule here would drift from it, so /yolo off asks the
// same owner again (printmode.PrintModeSandbox) and takes its answer.
//
// # Under the flag
//
// A session started with --dangerously-skip-permissions has the gates
// off as its floor, so /yolo has nothing to turn on and /yolo off
// cannot re-arm them. Both answer honestly instead of pretending.

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/printmode"
)

// slashYolo implements /yolo and /yolo [on|off].
//
// The bare form is the quick mode: /yolo means /yolo on, the way it reads
// ("just do it"). Turning the gates back on is always the explicit
// /yolo off — a bare repeat is not a toggle, because a user typing /yolo
// a second time means "keep going", not "stop".
//
// The command exists so a session can start normally — sandbox on,
// permissions asked — and drop the gates once the user has seen what the
// agent is doing, without restarting the binary and losing the
// conversation.
func (m *RootModel) slashYolo(args string) (bool, tea.Cmd) {
	if m.session.remoteAgent {
		m.slashRespond("/yolo", "Remote agents do not run a local permission gate; /permissions <preset> is their switch.")
		return true, nil
	}
	if m.session.policy == nil {
		m.slashRespond("/yolo", "No permission policy is armed in this session.")
		return true, nil
	}

	current := m.session.policy.SkipPermissions
	v := strings.TrimSpace(strings.ToLower(slashparse.Trim(args)))
	var target bool
	switch v {
	case "":
		// The quick mode: /yolo is /yolo on.
		target = true
	case "on", "enable", "true":
		target = true
	case "off", "disable", "false":
		target = false
	default:
		m.slashRespond("/yolo", "Usage: /yolo [on|off]")
		return true, nil
	}

	if target == current {
		state := "off"
		if target {
			state = "on"
		}
		m.slashRespond("/yolo", "Already "+state+".")
		return true, nil
	}

	if !target && cliflags.BoolValue(m.session.flags, "dangerously-skip-permissions") {
		m.slashRespond("/yolo", "Started with --dangerously-skip-permissions; /yolo cannot re-arm the gates.")
		return true, nil
	}

	m.setYolo(target)
	if target {
		m.slashRespond("/yolo", "Yolo on: no approval prompts, sandbox is off. Commands may now run outside the workspace.")
		return true, nil
	}
	m.slashRespond("/yolo", "Yolo off: approval prompts back, sandbox is back on.")
	return true, nil
}

// setYolo flips both gates the startup flag controls and rebuilds the
// tool surface so the value copies pick the change up.
func (m *RootModel) setYolo(on bool) {
	m.applyYoloState(on)
	m.rebuildToolSurface()
}

// applyYoloState flips the two gates without rebuilding anything.
//
// It is split from setYolo for two reasons. A test can pin the state
// transition on its own, and setYolo's rebuild touches the catalogue —
// a permission switch has no business reading the MCP configuration
// from the config home, which is what a full reloadPluginsAndMCP would
// also do.
func (m *RootModel) applyYoloState(on bool) {
	if on {
		m.tools.sandbox.Enabled = false
	} else {
		// The same owner that built the startup sandbox, asked again:
		// one rule (settings.json's switch plus the flag), one answer.
		m.tools.sandbox = printmode.PrintModeSandbox(m.session.flags, m.yoloWorkspace(), m.session.conversationID)
	}
	m.session.policy.SkipPermissions = on
	// The policy carries the sandbox flag too: the proceed-in-sandbox
	// toolPermission auto-approves a command only while the sandbox runs
	// it, so the two must not disagree. It is set from the sandbox the
	// switch just produced, not from its old value — reading the old one
	// reported the sandbox as off right after /yolo restored it.
	m.session.policy.SandboxEnabled = m.tools.sandbox.Enabled
}

// yoloWorkspace is the directory the sandbox rule resolves against.
func (m *RootModel) yoloWorkspace() string {
	if m.session.cwd != "" {
		return m.session.cwd
	}
	wd, _ := os.Getwd()
	return wd
}
