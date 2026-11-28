package tui

// slash_yolo_test.go — /yolo, the mid-session --dangerously-skip-permissions.
//
// The command is a *two-gate* switch, and the tests below pin both
// gates separately because turning off only one is the failure mode
// that looks like success: with the policy permissive but the sandbox
// still on, run_command dies at SandboxBoundaryMiddleware without ever
// reaching Policy.DecideCall, so a test that only reads
// policy.SkipPermissions would pass on a broken /yolo.

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// boolLiteral renders b as the JSON literal settings.json expects.
func boolLiteral(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// yoloTestModel builds the minimum RootModel /yolo touches: a policy, a
// sandbox and a flag set, all in a temporary home so the settings.json
// reads stay off the machine's real config.
func yoloTestModel(t *testing.T, args []string, sandboxOn bool) *RootModel {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("OPENAGY_ACCOUNT", "")

	// The sandbox's startup value is settings.json's switch, so a test
	// that expects it restored by /yolo off has to seed the same answer
	// the real setup would read. Without this the rule correctly says
	// "off", and the assertion would be checking the test's own default.
	seed := `{"enableTerminalSandbox": ` + boolLiteral(sandboxOn) + `}`
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed settings.json: %v", err)
	}

	fs := flag.NewFlagSet("yolo-test", flag.ContinueOnError)
	fs.Bool("dangerously-skip-permissions", false, "")
	fs.Bool("sandbox", false, "")
	fs.String("mode", "", "")
	fs.String("acp", "", "")
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}

	policy := &permissions.Policy{Mode: permissions.ModeAuto, SandboxEnabled: sandboxOn}
	m := &RootModel{}
	m.session.policy = policy
	m.session.flags = fs
	m.session.mode = permissions.ModeAuto
	m.tools.sandbox = sandbox.Sandbox{Enabled: sandboxOn, Cwd: home}
	return m
}

// TestYoloTurnsOffBothGates is the core contract: /yolo must switch the
// policy *and* the sandbox, because either one alone still blocks a
// command.
func TestYoloTurnsOffBothGates(t *testing.T) {
	m := yoloTestModel(t, nil, true)
	if !m.tools.sandbox.Enabled {
		t.Fatal("precondition: the sandbox should start enabled")
	}

	if handled, _ := m.slashYolo(""); !handled {
		t.Fatal("/yolo was not handled")
	}

	if !m.session.policy.SkipPermissions {
		t.Error("policy.SkipPermissions = false after /yolo; permissions are still asked")
	}
	if m.tools.sandbox.Enabled {
		t.Error("sandbox.Enabled = true after /yolo; commands outside the workspace still die at the sandbox gate")
	}
	if m.session.policy.SandboxEnabled {
		t.Error("policy.SandboxEnabled = true; the policy would auto-approve for a sandbox that is off")
	}
}

// TestYoloOffRestoresBothGates checks the reverse, and that the sandbox
// comes back from the same owner that built it rather than from a
// second copy of the rule.
func TestYoloOffRestoresBothGates(t *testing.T) {
	m := yoloTestModel(t, nil, true)
	if _, _ = m.slashYolo("on"); !m.session.policy.SkipPermissions {
		t.Fatal("/yolo on did not arm skip-permissions")
	}

	if _, _ = m.slashYolo("off"); m.session.policy.SkipPermissions {
		t.Error("policy.SkipPermissions = true after /yolo off")
	}
	if !m.session.policy.SandboxEnabled {
		t.Error("policy.SandboxEnabled = false after /yolo off; the proceed-in-sandbox verdict path disagrees with the sandbox")
	}
}

// TestYoloBareFormMeansOn: /yolo is the quick mode, not a toggle. A user
// typing it again means "keep going", so a repeat must stay on rather
// than silently re-arming the gates they turned off.
func TestYoloBareFormMeansOn(t *testing.T) {
	m := yoloTestModel(t, nil, true)

	if _, _ = m.slashYolo(""); !m.session.policy.SkipPermissions {
		t.Fatal("bare /yolo did not turn the gates off")
	}
	if m.tools.sandbox.Enabled {
		t.Fatal("bare /yolo left the sandbox on")
	}

	// The repeat is idempotent: still on, and it says so.
	m.conversation.response = nil
	if _, _ = m.slashYolo(""); !m.session.policy.SkipPermissions {
		t.Error("a second bare /yolo turned the gates back on; it must mean on")
	}
	if !responseContains(m, "Already on.") {
		t.Errorf("a repeat of bare /yolo should answer Already on.:\n%q", m.conversation.response)
	}

	// Only the explicit off form re-arms them.
	if _, _ = m.slashYolo("off"); m.session.policy.SkipPermissions {
		t.Error("/yolo off did not turn the gates back on")
	}
}

// TestYoloRejectsAnUnknownArgument pins the usage line: a typo must not
// be read as "on".
func TestYoloRejectsAnUnknownArgument(t *testing.T) {
	m := yoloTestModel(t, nil, true)
	if _, _ = m.slashYolo("yes"); m.session.policy.SkipPermissions {
		t.Error(`/yolo yes armed skip-permissions; want the usage line and no change`)
	}
	if !responseContains(m, "Usage: /yolo [on|off]") {
		t.Errorf("transcript missing the usage line:\n%q", m.conversation.response)
	}
	if !m.tools.sandbox.Enabled {
		t.Error("a rejected argument changed the sandbox")
	}
}

// TestYoloCannotRaiseTheFloorUnderTheFlag pins the honest answer when
// the session started with --dangerously-skip-permissions: the gates
// are already off and cannot be turned back on by this command.
func TestYoloCannotRaiseTheFloorUnderTheFlag(t *testing.T) {
	m := yoloTestModel(t, []string{"--dangerously-skip-permissions"}, false)
	m.session.policy.SkipPermissions = true // what setup.PolicyFrom would have produced

	if _, _ = m.slashYolo("off"); !m.session.policy.SkipPermissions {
		t.Error("/yolo off re-armed the gates under --dangerously-skip-permissions")
	}
	if !responseContains(m, "--dangerously-skip-permissions") {
		t.Errorf("/yolo off did not say why it cannot re-arm the gates:\n%q", m.conversation.response)
	}
}

// TestYoloIsRegisteredAndVisible: the command is advertised in the
// picker, not hidden — a user has to be able to find it.
func TestYoloIsRegisteredAndVisible(t *testing.T) {
	m := yoloTestModel(t, nil, false)
	if !m.dispatchesCommand("yolo") {
		t.Fatal("the host has no handler for yolo")
	}
	if !listed("yolo") {
		t.Error("/yolo is not advertised in the picker; it is meant to be a visible command")
	}
}

// TestYoloOffLeavesNothingPermissiveWhenNeverEnabled guards the
// baseline: /yolo off on a session that never switched on must not
// invent permission.
func TestYoloOffLeavesNothingPermissiveWhenNeverEnabled(t *testing.T) {
	m := yoloTestModel(t, nil, true)
	if _, _ = m.slashYolo("off"); m.session.policy.SkipPermissions {
		t.Error("/yolo off armed skip-permissions on a session that never had it")
	}
	if !responseContains(m, "Already off.") {
		t.Errorf("/yolo off on an untouched session should answer Already off.:\n%q", m.conversation.response)
	}
}

// TestYoloArgumentHintAndTabCompletion pins the argument surface: the
// composer hints "[on|off]" and Tab completes the word.
func TestYoloArgumentHintAndTabCompletion(t *testing.T) {
	if hint, ok := tuilib.ComposerArgumentHint("/yolo "); !ok || hint != "[on|off]" {
		t.Errorf(`ComposerArgumentHint("/yolo ") = (%q, %v), want ("[on|off]", true)`, hint, ok)
	}
	if hint, ok := tuilib.ComposerArgumentHint("/yolo o"); !ok || hint != "n" {
		t.Errorf(`ComposerArgumentHint("/yolo o") = (%q, %v), want ("n", true)`, hint, ok)
	}
	if hint, ok := tuilib.ComposerArgumentHint("/yolo"); !ok || hint != " on" {
		t.Errorf(`ComposerArgumentHint("/yolo") = (%q, %v), want (" on", true)`, hint, ok)
	}

	for _, tc := range []struct {
		value string
		want  string
	}{
		{"/yolo", "/yolo "},
		{"/yolo o", "/yolo on"},
		{"/yolo of", "/yolo off"},
	} {
		got, ok := completeWordArg(tc.value, "/yolo", []string{"on", "off"})
		if !ok || got != tc.want {
			t.Errorf("completeWordArg(%q) = (%q, %v), want (%q, true)", tc.value, got, ok, tc.want)
		}
	}
	// An already-complete or unknown word is left alone.
	for _, value := range []string{"/yolo on", "/yolo off", "/yolo xyz", "/yolo on extra"} {
		if got, ok := completeWordArg(value, "/yolo", []string{"on", "off"}); ok {
			t.Errorf("completeWordArg(%q) = (%q, true), want no completion", value, got)
		}
	}
}

// TestYoloOffIsIdempotentOnTheSandbox guards the rebuild path: turning
// the gates off twice must not leave the sandbox in a state the startup
// rule would not produce.
func TestYoloOffIsIdempotentOnTheSandbox(t *testing.T) {
	m := yoloTestModel(t, nil, true)
	if _, _ = m.slashYolo("on"); !m.session.policy.SkipPermissions {
		t.Fatal("/yolo on did not arm skip-permissions")
	}
	if _, _ = m.slashYolo("off"); m.session.policy.SkipPermissions {
		t.Fatal("/yolo off did not restore")
	}
	first := m.tools.sandbox.Enabled
	if _, _ = m.slashYolo("off"); m.session.policy.SkipPermissions {
		t.Fatal("/yolo off twice armed skip-permissions")
	}
	if m.tools.sandbox.Enabled != first {
		t.Errorf("sandbox.Enabled changed on a repeated /yolo off: %v -> %v", first, m.tools.sandbox.Enabled)
	}
}

// TestYoloSaysWhatItDid keeps the transcript honest: the two answers
// must differ, because they mean different things to the user.
func TestYoloSaysWhatItDid(t *testing.T) {
	on := yoloTestModel(t, nil, true)
	if _, _ = on.slashYolo("on"); !responseContains(on, "sandbox is off") {
		t.Errorf("/yolo on did not mention the sandbox:\n%q", on.conversation.response)
	}

	off := yoloTestModel(t, nil, true)
	if _, _ = off.slashYolo("on"); !off.session.policy.SkipPermissions {
		t.Fatal("/yolo on did not arm skip-permissions")
	}
	off.conversation.response = nil
	if _, _ = off.slashYolo("off"); !responseContains(off, "sandbox is back on") {
		t.Errorf("/yolo off did not mention restoring the sandbox:\n%q", off.conversation.response)
	}
}

// TestYoloTabCompletionEndToEnd drives the real key path, the way
// TestSwarmTabCompletion does: the picker owns Tab while it is visible,
// and the argument completion has to survive that hand-off.
func TestYoloTabCompletionEndToEnd(t *testing.T) {
	m := diffTestModel()
	initRootModelInput(m)
	m.prompt.picker = m.newSlashPicker()

	// /yol + Tab resolves the command name and leaves the argument open.
	m.prompt.input.SetValue("/yol")
	m.prompt.picker.Update([]rune("/yol"))
	if handled := m.handleTabCompletion(tea.Key{Code: tea.KeyTab}); !handled {
		t.Fatal("Tab on /yol was not handled")
	}
	if got := m.prompt.input.Value(); got != "/yolo " {
		t.Fatalf("Tab on /yol left %q, want %q", got, "/yolo ")
	}

	// A typed prefix completes to the word.
	m.prompt.input.SetValue("/yolo o")
	if handled := m.handleTabCompletion(tea.Key{Code: tea.KeyTab}); !handled {
		t.Fatal("Tab on '/yolo o' was not handled")
	}
	if got := m.prompt.input.Value(); got != "/yolo on" {
		t.Fatalf("Tab on '/yolo o' left %q, want %q", got, "/yolo on")
	}
}

// TestYoloOnARemoteSessionExplainsItself: the remote agent has no local
// gate, so the command must not pretend to flip one.
func TestYoloOnARemoteSessionExplainsItself(t *testing.T) {
	m := yoloTestModel(t, nil, false)
	m.session.remoteAgent = true
	if _, _ = m.slashYolo("on"); m.session.policy.SkipPermissions {
		t.Error("/yolo on armed a local policy on a remote-agent session")
	}
	if !responseContains(m, "/permissions") {
		t.Errorf("remote /yolo should point at /permissions:\n%q", m.conversation.response)
	}
}
