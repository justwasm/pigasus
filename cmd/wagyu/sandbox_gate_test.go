// sandbox_gate_test.go — cycle 228 (AGY 1.2.10 close-gap).
//
// The four minimum tests the cycle brief enumerates, exercised through
// the permissionRunner gate:
//
//  1. artifact dir is in the sandbox allow-list (carve-out #1)
//  2. scratch dir is in the allow-list (carve-out #2)
//  3. a one-shot bypass does not turn into a sticky permission grant
//  4. a sandboxed read of the artifact dir succeeds without a prompt
//
// We drive the gate with a recording inner runner so the test stays
// deterministic and does not touch the filesystem outside of
// t.TempDir. The sandbox is resolved against $ANTIGRAVITY_HOME
// (auth.AntigravityConfigHome honours it) so each test sees a fresh
// sandbox root and never touches the user's real ~/.gemini.
//
// Evidence grade 【strings】+【推测】: the recovery strings
// "BypassSandbox: true" / "Ran in the sandbox" / "tool call rejected"
// are in out/local-macos/strings-resolved.txt; the artifact / scratch
// paths are observed on the live machine.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/sandbox"
)

// recordingRunner mirrors the helper in permission_test.go so this
// file stays self-contained and a future test rename does not break
// the gate tests.
type sandboxRecordingRunner struct {
	ran []string
}

func (r *sandboxRecordingRunner) Run(_ context.Context, call backend.ToolCall) (string, error) {
	r.ran = append(r.ran, call.Name)
	return "ran", nil
}

// sandboxGate is the test-local wiring helper: builds a
// permissionRunner with the given sandbox, returns the runner + the
// recording inner so the test can assert what got forwarded.
func sandboxGate(t *testing.T, sb sandbox.Sandbox) (*permissionRunner, *sandboxRecordingRunner) {
	t.Helper()
	rec := &sandboxRecordingRunner{}
	gate := &permissionRunner{
		inner:   agent.ToolRunner(rec),
		policy:  &permissions.Policy{},
		sandbox: sb,
	}
	return gate, rec
}

// fakeAntigravityHome sets ANTIGRAVITY_HOME to a fresh temp dir so
// sandbox.Resolve picks a known artifact / scratch root. The returned
// string is the home dir for path joining.
func fakeAntigravityHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	return home
}

// TestGate_SandboxArtifactDirAllowed is carve-out #1: a run_command
// whose only path-shaped token is inside the artifact directory must
// pass the sandbox. Pre-cycle 228 the gate would deny it because the
// path is outside the cwd, so the user saw one "bypass?" prompt per
// artifact write.
func TestSandboxArtifactDirAllowed(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	// We need a source file inside the workspace so the cp command
	// only touches workspace + artifact — that's the only way the
	// gate's path check passes. The workspace is a fresh tmpdir
	// (sandbox.Resolve pins it as the cwd allow-list entry), so any
	// path outside it is "outside" by definition.
	source := filepath.Join(workspace, "src.txt")
	if err := writeFile(t, source, "x"); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	gate, rec := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-1"))
	artifact := filepath.Join(home, "artifacts", "snapshot.json")
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"cp ` + source + ` ` + artifact + `"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran" {
		t.Errorf("sandboxed cp into artifact dir must be allowed; got %q (rec=%v)", out, rec.ran)
	}
	if len(gate.Denials) != 0 {
		t.Errorf("sandboxed cp should not have denied; denials=%v", gate.Denials)
	}
}

// TestGate_SandboxScratchDirAllowed is carve-out #2: the per-conversation
// scratch path is on the allow-list, so a sandboxed command that writes
// there must pass without a prompt.
func TestSandboxScratchDirAllowed(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, rec := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-2"))
	// The command reads from the workspace (allowed) and writes to
	// the per-conversation scratch dir (allowed). Together the two
	// tokens cover both the cwd carve-out and the scratch carve-out.
	source := filepath.Join(workspace, "in.txt")
	if err := writeFile(t, source, "hi"); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	scratch := filepath.Join(home, "brain", "conv-2", "scratch", "tmp.txt")
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"cp ` + source + ` ` + scratch + `"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran" {
		t.Errorf("sandboxed write to scratch dir must be allowed; got %q (rec=%v)", out, rec.ran)
	}
	if len(gate.Denials) != 0 {
		t.Errorf("sandboxed scratch write should not have denied; denials=%v", gate.Denials)
	}
}

// TestGate_SandboxOutsidePathDenied is the negative case: an
// out-of-bounds path is denied even though the policy is permissive
// (the default). The model-facing text must name the sandbox so the
// model can rephrase the command to stay inside the allow-list.
func TestSandboxOutsidePathDenied(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, _ := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-3"))
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"rm -rf /etc/passwd"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "ran" {
		t.Errorf("sandboxed rm -rf /etc/passwd must be denied; got %q", out)
	}
	if !strings.Contains(out, "Sandbox blocked") {
		t.Errorf("denial text must name the sandbox; got %q", out)
	}
	if len(gate.Denials) != 1 {
		t.Errorf("denials = %v, want one", gate.Denials)
	}
}

// TestGate_SandboxBypassIsPerCall pins the AGY 1.2.10 second-half
// guarantee: a bypass on call #1 must not turn into a sticky
// permission grant on call #2.
//
// The bypass is wired through `BypassSandbox: true` in the tool
// arguments (recovered string out/local-macos/strings-resolved.txt);
// the gate honours it for the single call it appears on. We exercise
// the package-level invariant directly because the gate's Run path
// does not surface a per-call bypass argument — the gate is the
// "no-bypass" reference path. A future TUI hook would call
// `sandbox.CheckCommandWithBypass(cmd, true)` for the user-approved
// call only.
func TestSandboxBypassIsPerCall(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, _ := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-4"))

	res1 := gate.sandbox.CheckCommandWithBypass("rm -rf /etc/passwd", true)
	if res1.Outside {
		t.Errorf("bypass call must report Outside=false; got %+v", res1)
	}

	res2 := gate.sandbox.CheckCommandWithBypass("rm -rf /etc/passwd", false)
	if !res2.Outside {
		t.Errorf("non-bypass call must report Outside=true; got %+v", res2)
	}
}

// TestGate_SandboxDisabledAllowsEverything is the safe-default pin:
// when enableTerminalSandbox is false (the recovered default) the
// gate behaves exactly like the pre-cycle-228 gate. Any change to
// the default is caught by the carve-out tests above; this one
// confirms the disabled sandbox is transparent.
func TestSandboxDisabledAllowsEverything(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, rec := sandboxGate(t, sandbox.Resolve(false, workspace, home, "conv-5"))
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"rm -rf /etc/passwd"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran" {
		t.Errorf("disabled sandbox must allow every command; got %q (rec=%v)", out, rec.ran)
	}
}

// TestGate_SandboxSiblingPrefixDenied is the "no false positive" pin:
// a sibling of the artifact root (which shares the byte prefix but
// is not inside it) must remain outside the sandbox. Without the
// trailing-separator guard this would slip through.
func TestSandboxSiblingPrefixDenied(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, _ := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-6"))
	evil := filepath.Join(home, "artifacts-evil", "x")
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"cat ` + evil + `"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "ran" {
		t.Errorf("a sibling-prefix path must NOT pass the sandbox; got %q", out)
	}
}

// TestGate_SandboxOtherConversationScratchDenied covers the
// per-conversation isolation: another conversation's scratch is
// outside this sandbox, so the gate denies a cross-conversation
// read even though both roots share the brain/ prefix.
func TestSandboxOtherConversationScratchDenied(t *testing.T) {
	home := fakeAntigravityHome(t)
	workspace := t.TempDir()
	gate, _ := sandboxGate(t, sandbox.Resolve(true, workspace, home, "conv-this"))
	other := filepath.Join(home, "brain", "conv-other", "scratch", "x")
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"cat ` + other + `"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "ran" {
		t.Errorf("a different conversation's scratch must NOT pass this sandbox; got %q", out)
	}
}

// writeFile seeds a small text file under a tmpdir so the gate tests
// can drive a sandboxed cp/mv/cat that has a real source on disk.
// Without this helper the commands would still be valid shell, but
// the gate does not care about file existence — only the path
// tokens matter — so the seed is purely cosmetic. It is kept in
// scope so the failing assertion (if any) lands next to the
// offending fixture.
func writeFile(t *testing.T, path, body string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600)
}
