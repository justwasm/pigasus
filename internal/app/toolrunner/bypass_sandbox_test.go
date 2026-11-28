// bypass_sandbox_test.go — a call that asks to leave the terminal sandbox
// must be judged by what it wants to run, and must reach the user before it
// runs there.
//
// The defect both tests below pin: CheckCommandWithBypass answers "not
// outside" for a bypassed call because it skips the check, and the gates
// read that answer as "this command is fine". So every call carrying
// `BypassSandbox: true` ran outside the sandbox with nobody asked — while
// <terminal_sandbox> told the model that a bypass "requires manual user
// approval".

package toolrunner

import (
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
)

// blockedCall is a run_command the sandbox refuses: it reads a file the
// default allow-list does not cover.
func blockedCall(bypass bool) backend.ToolCall {
	args := `{"CommandLine":"cat /etc/shadow","Cwd":"/tmp/ws","WaitMsBeforeAsync":1000,"toolSummary":"read","toolAction":"reading"`
	if bypass {
		args += `,"BypassSandbox":true`
	}
	args += `}`
	return backend.ToolCall{Name: "run_command", Arguments: []byte(args)}
}

// TestDecideAsksBeforeRunningOutsideTheSandbox — the decision half routes a
// needed bypass to the user instead of allowing it.
func TestDecideAsksBeforeRunningOutsideTheSandbox(t *testing.T) {
	sb := sandbox.Sandbox{Enabled: true, Cwd: "/tmp/ws"}
	policy := &permissions.Policy{}

	if g := Decide(policy, sb, blockedCall(true)); !g.Ask || g.Allow {
		t.Errorf("a needed bypass was not sent to the user: Allow=%v Ask=%v (%s)", g.Allow, g.Ask, g.Reason)
	} else if !strings.Contains(g.Reason, "outside the terminal sandbox") {
		t.Errorf("the question does not say what it is asking: %q", g.Reason)
	}

	// Without the bypass the command is simply blocked; nothing to ask.
	if g := Decide(policy, sb, blockedCall(false)); g.Ask || g.Allow {
		t.Errorf("a blocked command without a bypass asked or allowed: Allow=%v Ask=%v", g.Allow, g.Ask)
	}

	// A command that runs sandboxed is allowed with or without the flag: the
	// bypass is a fallback, not a mode.
	for _, bypass := range []bool{false, true} {
		ok := backend.ToolCall{Name: "run_command", Arguments: []byte(
			`{"CommandLine":"ls -la","Cwd":"/tmp/ws","WaitMsBeforeAsync":1000,"toolSummary":"list","toolAction":"listing"}` +
				map[bool]string{false: "}", true: `,"BypassSandbox":true}`}[bypass])}
		if g := Decide(policy, sb, ok); !g.Allow {
			t.Errorf("a sandboxed command was refused (bypass=%v): %+v", bypass, g)
		}
	}
}

// TestPipelineAsksThenRunsOnApproval — the execution gate asks, and an
// approval is what lets the command run.
func TestPipelineAsksThenRunsOnApproval(t *testing.T) {
	sb := sandbox.Sandbox{Enabled: true, Cwd: "/tmp/ws"}

	run := func(approve bool) (asked bool, ran bool, out string) {
		base := SandboxBoundaryMiddlewareWithMode(sb, permissions.ModeDefault,
			func(context.Context, backend.ToolCall) (bool, error) {
				asked = true
				return approve, nil
			}, nil)
		h := base(func(context.Context, backend.ToolCall) (string, error) {
			ran = true
			return "ran", nil
		})
		out, _ = h(context.Background(), blockedCall(true))
		return
	}

	asked, ran, _ := run(true)
	if !asked || !ran {
		t.Errorf("an approved bypass did not run: asked=%v ran=%v", asked, ran)
	}

	asked, ran, out := run(false)
	if !asked {
		t.Error("a refused bypass never reached the user")
	}
	if ran {
		t.Error("a refused bypass ran the command anyway")
	}
	if !strings.Contains(out, "outside the terminal sandbox") {
		t.Errorf("the refusal does not say why: %q", out)
	}
}

// TestPipelineDoesNotAskForASandboxedCommand — a bypass flag on a command
// that runs sandboxed is not an escalation, so the user is not bothered.
func TestPipelineDoesNotAskForASandboxedCommand(t *testing.T) {
	sb := sandbox.Sandbox{Enabled: true, Cwd: "/tmp/ws"}
	asked := false
	base := SandboxBoundaryMiddlewareWithMode(sb, permissions.ModeDefault,
		func(context.Context, backend.ToolCall) (bool, error) {
			asked = true
			return false, nil
		}, nil)
	h := base(func(context.Context, backend.ToolCall) (string, error) { return "ran", nil })
	call := backend.ToolCall{Name: "run_command", Arguments: []byte(
		`{"CommandLine":"ls -la","Cwd":"/tmp/ws","WaitMsBeforeAsync":1000,"toolSummary":"list","toolAction":"listing","BypassSandbox":true}`)}
	if _, err := h(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	if asked {
		t.Error("a command that runs sandboxed nonetheless asked for a bypass")
	}
}
