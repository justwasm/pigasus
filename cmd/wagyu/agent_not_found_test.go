// agent_not_found_test.go — cycle agent-not-found-exit-1
//
// Integration tests that drive /tmp/wagyu-anf through the full
// CLI parsing layer. The unit tests in internal/agent cover the
// catalogue and the typed error; these tests pin the cmd/wagyu
// wiring — the place the changelog gap actually lives.
//
// The tests skip when the binary is not present (CI without
// `go build -o /tmp/wagyu-anf ./cmd/wagyu`) so the suite stays
// runnable in environments that don't have the binary built.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// wagyuBinary is the path the cycle's build step produces. The test
// suite skips when the binary is absent (e.g. fresh `go test` without
// a build). The Makefile builds the binary before `go test`; the
// worktree setup used by this cycle (`go build -o /tmp/wagyu-anf
// ./cmd/wagyu`) puts it on the standard search path.
const wagyuBinary = "/tmp/wagyu-anf"

func requireWagyuBinary(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(wagyuBinary); err != nil {
		t.Skipf("binary %s not present; run `go build -o %s ./cmd/wagyu` first: %v",
			wagyuBinary, wagyuBinary, err)
	}
}

// runWagyu executes the cycle's binary with the given args and
// returns (stdout, stderr, exitCode).
func runWagyu(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(wagyuBinary, args...)
	// Force the offline shim: these tests exercise flag parsing and the
	// unknown-agent error, not transport selection. Without this a machine
	// with a live OAuth token sends the happy-path turns to CloudCode and
	// the outcome depends on account location/quota.
	cmd.Env = append(os.Environ(), "OPENAGY_BACKEND=shim")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("unexpected run error (non-ExitError): %v", err)
	}
	return stdout.String(), stderr.String(), code
}

// exitCode returns err's process exit code, or 0 when err is nil.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// TestAgentFlag_ValidNameContinues pins the happy path: `--agent
// default -p "hi"` runs the print mode and exits 0. The mock backend
// returns the canonical "Hello! 👋 How can I help you today?" reply.
func TestAgentFlag_ValidNameContinues(t *testing.T) {
	requireWagyuBinary(t)
	out, _, code := runWagyu(t, "-p", "hi", "--agent", "default")
	if code != 0 {
		t.Errorf("--agent default -p hi exit code = %d, want 0", code)
	}
	if out == "" {
		t.Errorf("--agent default -p hi: stdout is empty")
	}
	// Sanity: stdout should be a non-empty response from the mock
	// backend. The mock's wording has evolved across cycles, so only
	// require that a reply was produced.
	if strings.TrimSpace(out) == "" {
		t.Errorf("--agent default -p hi stdout is empty")
	}
}

// TestAgentFlag_MissingContinues covers the absent-flag case: no
// --agent at all means "default agent", and `-p "hi"` should
// continue normally with exit 0.
func TestAgentFlag_MissingContinues(t *testing.T) {
	requireWagyuBinary(t)
	out, _, code := runWagyu(t, "-p", "hi")
	if code != 0 {
		t.Errorf("no --agent -p hi exit code = %d, want 0", code)
	}
	if out == "" {
		t.Errorf("no --agent -p hi: stdout is empty")
	}
}

// TestAgentFlag_EmptyValueContinues covers the `--agent=` form (no
// value): behaves identically to omitting the flag.
func TestAgentFlag_EmptyValueContinues(t *testing.T) {
	requireWagyuBinary(t)
	out, _, code := runWagyu(t, "-p", "hi", "--agent=")
	if code != 0 {
		t.Errorf("--agent= -p hi exit code = %d, want 0", code)
	}
	if out == "" {
		t.Errorf("--agent= -p hi: stdout is empty")
	}
}

// TestAgentFlag_UnknownInPrintMode_ExitsOne is the headline
// changelog-gap fix: `--agent nosuchagent -p "hi"` must exit 1 with
// the reference's unknown-agent error. This is the test that
// directly mirrors the brief's first acceptance criterion.
func TestAgentFlag_UnknownInPrintMode_ExitsOne(t *testing.T) {
	requireWagyuBinary(t)
	const missing = "nosuchagent"
	out, errb, code := runWagyu(t, "-p", "hi", "--agent", missing)

	if code != 1 {
		t.Errorf("--agent %s -p hi exit code = %d, want 1", missing, code)
	}
	if out != "" {
		t.Errorf("--agent %s -p hi: stdout should be empty, got %q", missing, out)
	}
	wantMsg := `error: unknown agent "` + missing + `" (no custom agents are available)`
	if !strings.Contains(errb, wantMsg) {
		t.Errorf("--agent %s -p hi: stderr missing %q\ngot: %q", missing, wantMsg, errb)
	}
	// The message must include the verbatim name. The brief
	// spells this out: "error message includes the missing name verbatim".
	if !strings.Contains(errb, missing) {
		t.Errorf("--agent %s -p hi: stderr missing the verbatim name %q\ngot: %q",
			missing, missing, errb)
	}
	// With no custom agents on disk the reference says so explicitly; the
	// built-in default is never listed.
	if strings.Contains(errb, "Available agents:") || strings.Contains(errb, "available: default") {
		t.Errorf("--agent %s -p hi: default must not be listed\ngot: %q", missing, errb)
	}
}

// TestAgentFlag_UnknownInPrintMode_LongForm covers the long-form
// alias: `--print "hi" --agent nosuchagent` is the same as
// `-p "hi" --agent nosuchagent`.
func TestAgentFlag_UnknownInPrintMode_LongForm(t *testing.T) {
	requireWagyuBinary(t)
	const missing = "weirdagent"
	out, errb, code := runWagyu(t, "--print", "hi", "--agent", missing)
	if code != 1 {
		t.Errorf("--print --agent %s exit code = %d, want 1", missing, code)
	}
	if out != "" {
		t.Errorf("--print --agent %s: stdout should be empty, got %q", missing, out)
	}
	if !strings.Contains(errb, missing) {
		t.Errorf("--print --agent %s: stderr missing the verbatim name %q\ngot: %q",
			missing, missing, errb)
	}
}

// TestAgentFlag_UnknownInPrintMode_PromptAlias covers the
// `--prompt` alias — same behaviour as `-p` / `--print`.
func TestAgentFlag_UnknownInPrintMode_PromptAlias(t *testing.T) {
	requireWagyuBinary(t)
	const missing = "not-real"
	_, errb, code := runWagyu(t, "--prompt", "hi", "--agent", missing)
	if code != 1 {
		t.Errorf("--prompt --agent %s exit code = %d, want 1", missing, code)
	}
	if !strings.Contains(errb, missing) {
		t.Errorf("--prompt --agent %s: stderr missing the verbatim name %q\ngot: %q",
			missing, missing, errb)
	}
}

// TestAgentsCLISubcommand_StillWorks pins the brief's last
// acceptance criterion: the `agents` CLI subcommand must continue
// working after the catalogue refactor. We don't render the whole
// subcommand path (it would require a TTY); we just check that
// the `agents` invocation does not error and the binary is
// runnable end-to-end.
func TestAgentsCLISubcommand_StillWorks(t *testing.T) {
	requireWagyuBinary(t)
	// `agents` with no args → empty stdout, exit 0 (the v1.3.5
	// binary prints nothing for `agents` without args, only
	// surfacing the mode-warning passthrough).
	out, _, code := runWagyu(t, "agents")
	if code != 0 {
		t.Errorf("agents exit code = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("agents stdout should be empty, got %q", out)
	}
}

// TestAgentFlag_AvailableList_UsesFrontmatterNames pins the error's
// custom-agent list: names come from the Markdown frontmatter (not the file
// name), are sorted, comma-joined, and exclude the built-in default.
func TestAgentFlag_AvailableList_UsesFrontmatterNames(t *testing.T) {
	requireWagyuBinary(t)
	ws := t.TempDir()
	home := t.TempDir()
	dir := filepath.Join(ws, ".agents", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for file, name := range map[string]string{"1.md": "zebra", "2.md": "alpha"} {
		body := "---\nname: " + name + "\ndescription: d\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(wagyuBinary, "-p", "hi", "--agent", "nosuch")
	cmd.Dir = ws
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if code := exitCode(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := `error: unknown agent "nosuch" (available: alpha, zebra)`; !strings.Contains(string(out), want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
}

// TestAgentFlag_InteractiveWarning covers the TUI half of the
// brief: when --agent NAME doesn't match and the user is on the
// interactive path (no -p), the binary warns and continues with
// the default agent. We can't actually drive the TUI without a
// real TTY, so we capture stderr from a process whose TUI launch
// fails (bubbletea: could not open TTY) and verify the warning
// fires correctly. The warning must:
//
//   - Use the "Agent Not Found:" banner title
//   - Include the missing name verbatim
//   - Use the "(no custom agents are available)" clause, never the
//     invented "Available agents:" list
//   - Continue with the "Using the default agent instead." tail
func TestAgentFlag_InteractiveWarning(t *testing.T) {
	requireWagyuBinary(t)
	// Use a fresh ANTIGRAVITY_HOME so the onboarding wizard does not
	// add noise to stderr; we want to pin the agent-validation
	// warning in isolation.
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	const missing = "nosuchagent"
	out, errb, code := runWagyu(t, "--agent", missing)

	// We don't care about the exit code (bubbletea exits 1 on TTY
	// failure in non-TTY tests; in a real TTY the TUI would launch
	// and use the default agent). We do care that the warning is
	// in stderr.
	_ = code
	if out != "" {
		t.Logf("--agent %s (no -p): stdout = %q", missing, out)
	}
	// The warning must appear in stderr. Use HasPrefix / Contains
	// rather than an exact match — bubbletea appends its own
	// diagnostics after the warning.
	if !strings.Contains(errb, "Agent Not Found:") {
		t.Errorf("--agent %s (no -p) stderr missing banner title:\n%q", missing, errb)
	}
	if !strings.Contains(errb, missing) {
		t.Errorf("--agent %s (no -p) stderr missing the verbatim name:\n%q", missing, errb)
	}
	if !strings.Contains(errb, "(no custom agents are available)") {
		t.Errorf("--agent %s (no -p) stderr missing the empty-catalogue clause:\n%q", missing, errb)
	}
	if strings.Contains(errb, "Available agents:") {
		t.Errorf("--agent %s (no -p) stderr still uses the old list:\n%q", missing, errb)
	}
	if !strings.Contains(errb, "Using the default agent instead.") {
		t.Errorf("--agent %s (no -p) stderr missing the fallback tail:\n%q", missing, errb)
	}
}
