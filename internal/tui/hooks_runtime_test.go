package tui

// hooks_runtime_test.go — the host's side of the documented hooks.
//
// The runtime itself is pinned by internal/core/policy/hooks. This file
// pins the host's glue: the gate the PreToolUse hooks sit in front of,
// the arguments they may replace, and the lifecycle events' own firing.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
)

// hookTestMode builds a run mode with one documented hook.
func hookTestMode(t *testing.T, command string) *runMode {
	t.Helper()
	m := newRunModeBase(nil, "", "gemini-3.8-flash", "", t.TempDir(), false)
	m.hookRT = &corehooks.Runtime{
		Registry: &corehooks.Registry{Entries: []corehooks.Entry{{
			Hook:    "test",
			Event:   corehooks.EventPreToolUse,
			Matcher: "*",
			Handler: corehooks.Handler{Type: "command", Command: command},
		}}},
		Shell: func(cmd string) []string { return []string{"/bin/sh", "-c", cmd} },
	}
	return m
}

// hookTestCall is the tool call the gate is asked about.
func hookTestCall() backend.ToolCall {
	return backend.ToolCall{Name: "run_command", Arguments: json.RawMessage(`{"CommandLine":"ls"}`)}
}

// TestHookToolGateDeny pins the blocking decision: the hook's own reason
// is the text the model reads.
func TestHookToolGateDeny(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"deny","reason":"blocked"}'`)
	gate := m.hookToolGate(context.Background(), hookTestCall())
	if !gate.Denied {
		t.Fatal("the gate allowed the call, want it blocked")
	}
	if gate.Reason != "blocked" {
		t.Errorf("reason = %q, want blocked", gate.Reason)
	}
}

// TestHookToolGateWithoutAReasonFallsBackToTheReferenceText pins the
// fallback: a block without a reason reads as the reference's own denial
// text.
func TestHookToolGateWithoutAReasonFallsBackToTheReferenceText(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"deny"}'`)
	gate := m.hookToolGate(context.Background(), hookTestCall())
	if !gate.Denied {
		t.Fatal("the gate allowed the call, want it blocked")
	}
	if !strings.Contains(gate.Reason, "was denied") {
		t.Errorf("reason = %q, want the reference's denial text", gate.Reason)
	}
}

// TestHookToolGateAllowDelegates pins the delegation: the decision
// "allow" leaves the call to the default policy.
func TestHookToolGateAllowDelegates(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"allow"}'`)
	gate := m.hookToolGate(context.Background(), hookTestCall())
	if gate.Denied || gate.Approved {
		t.Errorf("gate = %+v, want the policy to decide", gate)
	}
}

// TestHookToolGateAsk pins the two prompts: "ask" respects the user's
// own "always allow" grants, and "force_ask" does not.
func TestHookToolGateAsk(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"ask"}'`)
	gate := m.hookToolGate(context.Background(), hookTestCall())
	if !gate.Denied {
		t.Error("the gate allowed the call, want it prompted")
	}
	// With a grant of the user's own, the decision respects it.
	m.policy = &permissions.Policy{Allow: []permissions.Rule{{Tool: "run_command", Pattern: "ls"}}}
	gate = m.hookToolGate(context.Background(), hookTestCall())
	if gate.Denied {
		t.Error("the gate blocked a granted call, want it allowed")
	}
}

// TestHookToolGateForceAsk pins the decision that ignores the grants.
func TestHookToolGateForceAsk(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"force_ask"}'`)
	m.policy = &permissions.Policy{Allow: []permissions.Rule{{Tool: "run_command", Pattern: "ls"}}}
	gate := m.hookToolGate(context.Background(), hookTestCall())
	if !gate.Denied {
		t.Error("the gate allowed the call, want it prompted")
	}
}

// TestHookToolGateAutoApprove pins the approving decision: the hook
// approves the call, and the user's own deny rules still block it.
func TestHookToolGateAutoApprove(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"auto_approve"}'`)
	gate := m.hookToolGate(context.Background(), hookTestCall())
	if !gate.Approved {
		t.Errorf("gate = %+v, want the call approved", gate)
	}
	m.policy = &permissions.Policy{Deny: []permissions.Rule{{Tool: "run_command", Pattern: "ls"}}}
	gate = m.hookToolGate(context.Background(), hookTestCall())
	if !gate.Denied {
		t.Errorf("gate = %+v, want the denied target blocked", gate)
	}
}

// TestHookToolGateDenyUnlessPriorGrant pins the conditional block.
func TestHookToolGateDenyUnlessPriorGrant(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"deny_unless_prior_grant"}'`)
	gate := m.hookToolGate(context.Background(), hookTestCall())
	if !gate.Denied {
		t.Error("the gate allowed the call, want it blocked without a grant")
	}
	m.policy = &permissions.Policy{Allow: []permissions.Rule{{Tool: "run_command", Pattern: "ls"}}}
	gate = m.hookToolGate(context.Background(), hookTestCall())
	if !gate.Approved {
		t.Errorf("gate = %+v, want the granted call approved", gate)
	}
}

// TestHookToolGateWithoutHooks pins the gate of a session with no
// documented hooks: the policy decides everything.
func TestHookToolGateWithoutHooks(t *testing.T) {
	m := newRunModeBase(nil, "", "gemini-3.8-flash", "", t.TempDir(), false)
	gate := m.hookToolGate(context.Background(), hookTestCall())
	if gate.Denied || gate.Approved {
		t.Errorf("gate = %+v, want nothing", gate)
	}
}

// hookRecordingRunner is a tool runner that records the calls it ran.
type hookRecordingRunner struct {
	calls []backend.ToolCall
}

func (r *hookRecordingRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	r.calls = append(r.calls, call)
	return "ran", nil
}

// TestPermissionRunnerLetsTheHookBlockTheCall pins the gate's order: a
// hook that blocks the call stops it before the policy and before the
// tool.
func TestPermissionRunnerLetsTheHookBlockTheCall(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"deny","reason":"blocked"}'`)
	inner := &hookRecordingRunner{}
	runner := &permissionRunner{
		inner:  inner,
		policy: &permissions.Policy{},
		hook:   m.hookToolGate,
		post:   m.hookPostToolUse,
	}
	res, err := runner.Run(context.Background(), hookTestCall())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res != "blocked" {
		t.Errorf("result = %q, want blocked", res)
	}
	if len(inner.calls) != 0 {
		t.Errorf("the tool ran %d times, want 0", len(inner.calls))
	}
}

// TestPermissionRunnerLetsTheHookApproveTheCall pins the other
// direction: a hook that approves the call overrides the policy's own
// prompt.
func TestPermissionRunnerLetsTheHookApproveTheCall(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"auto_approve"}'`)
	// The policy would prompt for every call.
	policy := &permissions.Policy{Strict: true}
	inner := &hookRecordingRunner{}
	runner := &permissionRunner{
		inner:  inner,
		policy: policy,
		hook:   m.hookToolGate,
		post:   m.hookPostToolUse,
	}
	res, err := runner.Run(context.Background(), hookTestCall())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res != "ran" {
		t.Errorf("result = %q, want the tool to run", res)
	}
	if len(inner.calls) != 1 {
		t.Errorf("the tool ran %d times, want 1", len(inner.calls))
	}
	// The policy itself is untouched: the approval is the call's, not
	// the policy's.
	if !policy.Strict {
		t.Error("the approval changed the policy")
	}
}

// TestPermissionRunnerAppliesTheHookOverwrite pins the documented
// argument replacement: the merge is shallow.
func TestPermissionRunnerAppliesTheHookOverwrite(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"allow","overwrite":{"CommandLine":"ls -la"}}'`)
	inner := &hookRecordingRunner{}
	runner := &permissionRunner{
		inner:  inner,
		policy: &permissions.Policy{},
		hook:   m.hookToolGate,
	}
	if _, err := runner.Run(context.Background(), hookTestCall()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("the tool ran %d times, want 1", len(inner.calls))
	}
	args := string(inner.calls[0].Arguments)
	if !strings.Contains(args, "ls -la") {
		t.Errorf("arguments = %s, want the hook's replacement", args)
	}
}

// TestMergeHookArgs pins the merge itself: a shallow, top-level merge.
func TestMergeHookArgs(t *testing.T) {
	args := json.RawMessage(`{"CommandLine":"ls","Cwd":"/tmp"}`)
	got := mergeHookArgs(args, map[string]json.RawMessage{
		"CommandLine": json.RawMessage(`"ls -la"`),
	})
	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("the merged arguments are not JSON: %v", err)
	}
	if obj["CommandLine"] != "ls -la" {
		t.Errorf("CommandLine = %v, want the hook's replacement", obj["CommandLine"])
	}
	if obj["Cwd"] != "/tmp" {
		t.Errorf("Cwd = %v, want the call's own value", obj["Cwd"])
	}
	// Without an overwrite the call's own arguments are kept.
	if got := mergeHookArgs(args, nil); string(got) != string(args) {
		t.Errorf("mergeHookArgs without an overwrite = %s, want %s", got, args)
	}
}

// TestHookSessionStartFiresOnce pins the SessionStart event: it fires
// once per session.
func TestHookSessionStartFiresOnce(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"allow"}'`)
	m.hookRT.Registry.Entries = []corehooks.Entry{{
		Hook:    "test",
		Event:   corehooks.EventSessionStart,
		Handler: corehooks.Handler{Type: "command", Command: `echo '{"injectSteps":[]}'`},
	}}
	m.hookSessionStart(context.Background())
	m.hookSessionStart(context.Background())
	if m.hooks.Count(hooks.SessionStart) != 1 {
		t.Errorf("SessionStart fired %d times, want 1", m.hooks.Count(hooks.SessionStart))
	}
}

// TestPostToolUseFiresAfterTheCall pins the PostToolUse event: it fires
// after a tool completes, and not for a call that never ran.
func TestPostToolUseFiresAfterTheCall(t *testing.T) {
	m := hookTestMode(t, `echo '{"decision":"allow"}'`)
	runner := &permissionRunner{
		inner:  &hookRecordingRunner{},
		policy: &permissions.Policy{},
		hook:   m.hookToolGate,
		post:   m.hookPostToolUse,
	}
	if _, err := runner.Run(context.Background(), hookTestCall()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := m.hooks.Count(hooks.PostToolUse); got != 1 {
		t.Errorf("PostToolUse fired %d times, want 1", got)
	}
	// A call the policy blocked never ran, so it has no completion.
	runner.policy = &permissions.Policy{Strict: true}
	if _, err := runner.Run(context.Background(), hookTestCall()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := m.hooks.Count(hooks.PostToolUse); got != 1 {
		t.Errorf("PostToolUse fired %d times, want 1", got)
	}
}
