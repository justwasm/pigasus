package hooks

import (
	"context"
	"strings"
	"testing"
	"time"
)

// testShell runs a command through /bin/sh, so the test does not depend
// on the platform's usual shell.
func testShell(command string) []string {
	return []string{"/bin/sh", "-c", command}
}

// testRuntime builds a runtime over the given entries.
func testRuntime(entries ...Entry) *Runtime {
	return &Runtime{
		Registry: &Registry{Entries: entries},
		Shell:    testShell,
	}
}

// entry builds one entry for the test.
func entry(command string) Entry {
	return Entry{
		Hook:    "test",
		Event:   EventPreToolUse,
		Handler: Handler{Type: "command", Command: command},
	}
}

// TestFireRunsTheCommandWithTheInputOnStdin pins the input contract: the
// handler receives the input as JSON on stdin and answers with JSON on
// stdout.
func TestFireRunsTheCommandWithTheInputOnStdin(t *testing.T) {
	rt := testRuntime(entry(
		`id=$(cat); case "$id" in *conv-1*) echo '{"decision":"deny","reason":"seen"}' ;; *) echo '{}' ;; esac`))
	out := rt.Fire(context.Background(), EventPreToolUse, Input{ConversationID: "conv-1"})
	if out.Decision != DecisionDeny {
		t.Fatalf("decision = %q, want deny", out.Decision)
	}
	if out.Reason != "seen" {
		t.Errorf("reason = %q, want seen", out.Reason)
	}
	if out.Ran != 1 {
		t.Errorf("ran = %d, want 1", out.Ran)
	}
}

// TestFireWithoutADecision pins the delegation: a handler that writes no
// decision has no opinion, and the outcome stays the floor.
func TestFireWithoutADecision(t *testing.T) {
	rt := testRuntime(entry(`echo '{}'`))
	out := rt.Fire(context.Background(), EventPreToolUse, Input{})
	if out.Decision != DecisionAllow {
		t.Fatalf("decision = %q, want allow", out.Decision)
	}
	if out.Ran != 1 {
		t.Errorf("ran = %d, want 1", out.Ran)
	}
}

// TestFireTakesTheStrongestDecision pins the aggregation: a deny beats
// an allow, whatever order the handlers ran in.
func TestFireTakesTheStrongestDecision(t *testing.T) {
	rt := &Runtime{
		Registry: &Registry{Entries: []Entry{
			entry(`echo '{"decision":"allow"}'`),
			entry(`echo '{"decision":"deny","reason":"blocked"}'`),
		}},
		Shell: testShell,
	}
	out := rt.Fire(context.Background(), EventPreToolUse, Input{})
	if out.Decision != DecisionDeny {
		t.Fatalf("decision = %q, want deny", out.Decision)
	}
	if out.Reason != "blocked" {
		t.Errorf("reason = %q, want blocked", out.Reason)
	}
}

// TestFireTreatsAnUnknownDecisionAsAPrompt pins the conservative
// reading: an answer the documentation does not name is a prompt.
func TestFireTreatsAnUnknownDecisionAsAPrompt(t *testing.T) {
	rt := testRuntime(entry(`echo '{"decision":"maybe"}'`))
	out := rt.Fire(context.Background(), EventPreToolUse, Input{})
	if out.Decision != DecisionAsk {
		t.Fatalf("decision = %q, want ask", out.Decision)
	}
	if len(out.Problems) != 1 || !strings.Contains(out.Problems[0], "unknown decision") {
		t.Errorf("problems = %v, want the unknown decision", out.Problems)
	}
}

// TestFireRecordsAFailingHandler pins the tolerance: a handler that
// fails does not decide, and the failure is reported.
func TestFireRecordsAFailingHandler(t *testing.T) {
	rt := testRuntime(entry(`echo 'boom' >&2; exit 1`))
	out := rt.Fire(context.Background(), EventPreToolUse, Input{})
	if out.Ran != 0 {
		t.Errorf("ran = %d, want 0", out.Ran)
	}
	if len(out.Problems) != 1 || !strings.Contains(out.Problems[0], "boom") {
		t.Errorf("problems = %v, want the failure", out.Problems)
	}
	if out.Decision != DecisionAllow {
		t.Errorf("decision = %q, want allow", out.Decision)
	}
}

// TestFireRecordsUnusableJSON pins the second failure mode: a handler
// that writes something that is not JSON does not decide either.
func TestFireRecordsUnusableJSON(t *testing.T) {
	rt := testRuntime(entry(`echo 'not json'`))
	out := rt.Fire(context.Background(), EventPreToolUse, Input{})
	if len(out.Problems) != 1 || !strings.Contains(out.Problems[0], "invalid JSON") {
		t.Errorf("problems = %v, want the invalid JSON", out.Problems)
	}
}

// TestFireHonoursTheTimeout pins the handler's own timeout: a command
// that runs past it is killed and the failure is recorded.
func TestFireHonoursTheTimeout(t *testing.T) {
	e := entry(`sleep 5`)
	e.Handler.Timeout = 1
	rt := testRuntime(e)
	start := time.Now()
	out := rt.Fire(context.Background(), EventPreToolUse, Input{})
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("the command ran for %s, want the timeout to end it", elapsed)
	}
	if len(out.Problems) != 1 {
		t.Errorf("problems = %v, want the timeout", out.Problems)
	}
}

// TestFireTheStopEvent pins the Stop event's own answer: "continue"
// keeps the loop running, and any other value lets the agent stop.
func TestFireTheStopEvent(t *testing.T) {
	rt := testRuntime(Entry{
		Hook:    "stop",
		Event:   EventStop,
		Handler: Handler{Type: "command", Command: `echo '{"decision":"continue","reason":"not done"}'`},
	})
	out := rt.Fire(context.Background(), EventStop, Input{})
	if !out.Continue {
		t.Error("the Stop handler's continue answer was lost")
	}
	if out.Reason != "not done" {
		t.Errorf("reason = %q, want not done", out.Reason)
	}
}

// TestFireWithoutARegistry pins the empty runtime: it decides nothing
// and reports nothing.
func TestFireWithoutARegistry(t *testing.T) {
	var rt *Runtime
	if out := rt.Fire(context.Background(), EventPreToolUse, Input{}); out.Ran != 0 {
		t.Errorf("ran = %d, want 0", out.Ran)
	}
	empty := &Runtime{}
	if out := empty.Fire(context.Background(), EventPreToolUse, Input{}); out.Decision != DecisionAllow {
		t.Errorf("decision = %q, want allow", out.Decision)
	}
}

// TestFallbackShellArgv pins the fallback shell's shape: the command
// runs through the platform's usual shell.
func TestFallbackShellArgv(t *testing.T) {
	argv := FallbackShellArgv("echo hi")
	if len(argv) < 3 {
		t.Fatalf("FallbackShellArgv = %v, want the shell and the command", argv)
	}
	if argv[len(argv)-1] != "echo hi" {
		t.Errorf("FallbackShellArgv = %v, want the command last", argv)
	}
}

// TestHookInputFields_154 verifies that hookEventName, cwd, and toolCall.id
// are sent in the hook JSON input (AGY 1.5.4).
func TestHookInputFields_154(t *testing.T) {
	cmd := `input=$(cat); if echo "$input" | grep -q "hookEventName" && echo "$input" | grep -q "cwd" && echo "$input" | grep -q "call_999"; then echo '{"decision":"deny","reason":"matched_154"}'; else echo '{}'; fi`
	rt := testRuntime(entry(cmd))
	rt.WorkDir = t.TempDir()
	out := rt.Fire(context.Background(), EventPreToolUse, Input{
		ToolCall: &ToolCall{ID: "call_999", Name: "run_command"},
	})
	if out.Decision != DecisionDeny || out.Reason != "matched_154" {
		t.Fatalf("failed to match 1.5.4 hook input fields: decision=%v reason=%v problems=%v", out.Decision, out.Reason, out.Problems)
	}
}

// TestPluginHookEnvVars_154 verifies PLUGIN_ROOT and PLUGIN_DATA env injection
// and variable expansion for plugin hook commands (AGY 1.5.4).
func TestPluginHookEnvVars_154(t *testing.T) {
	e := Entry{
		Hook:       "plugin-hook",
		Event:      EventPreToolUse,
		PluginRoot: "/plugins/my-plugin",
		PluginData: "/data/my-plugin",
		Handler: Handler{
			Type:    "command",
			Command: `if [ "$PLUGIN_ROOT" = "/plugins/my-plugin" ] && [ "$PLUGIN_DATA" = "/data/my-plugin" ]; then echo '{"decision":"deny","reason":"env_ok"}'; else echo '{}'; fi`,
		},
	}
	rt := testRuntime(e)
	out := rt.Fire(context.Background(), EventPreToolUse, Input{})
	if out.Decision != DecisionDeny || out.Reason != "env_ok" {
		t.Fatalf("expected env vars injected: decision=%v reason=%v", out.Decision, out.Reason)
	}

	// Also verify placeholder expansion
	e2 := Entry{
		Hook:       "expand-hook",
		Event:      EventPreToolUse,
		PluginRoot: "/plugins/my-plugin",
		PluginData: "/data/my-plugin",
		Handler: Handler{
			Type:    "command",
			Command: `echo '{"decision":"deny","reason":"${PLUGIN_ROOT}"}'`,
		},
	}
	rt2 := testRuntime(e2)
	out2 := rt2.Fire(context.Background(), EventPreToolUse, Input{})
	if out2.Reason != "/plugins/my-plugin" {
		t.Fatalf("expected ${PLUGIN_ROOT} expanded, got %q", out2.Reason)
	}
}
