// trust_prompt_test.go — cycle 125: the workspace-trust gate.
//
// Covers renderTrustPrompt, runTrustPrompt, and trustGate. The gate
// test uses a fresh temp HOME so it does not pollute the real
// settings.json (the production path writes to
// ~/.gemini/antigravity-cli/settings.json, which is read by
// IsWorkspaceTrusted; cycle 124 already added a sandbox-only
// $ANTIGRAVITY_HOME override that auth.AntigravityConfigHome
// honours).
package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderTrustPromptSmoke confirms the dialog has every string
// recovered from the 1.3.6 string table (cycle 125 evidence). A
// regression here would mean the dialog is unrecognisable vs the
// reference and break the A/B harness's diff count.
func TestRenderTrustPromptSmoke(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTrustPrompt("/Users/gear/GitHub/openagy", &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	want := []string{
		"Trust this folder?",
		"Do you trust the contents of this project?",
		"Yes, I trust this folder",
		"~/GitHub/openagy", // ~-expanded for the user's eye
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("render output missing %q\nfull output:\n%s", w, out)
		}
	}
}

// TestRenderTrustPromptRawPath covers the non-home cwd branch:
// renderTrustPrompt must not panic on a path outside $HOME and
// must not invent a "~" prefix.
func TestRenderTrustPromptRawPath(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTrustPrompt("/tmp/agytui-probe", &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "/tmp/agytui-probe") {
		t.Errorf("non-home cwd should be displayed verbatim; got:\n%s", out)
	}
	if strings.Contains(out, "~/tmp") {
		t.Errorf("non-home cwd must not be ~-expanded; got:\n%s", out)
	}
}

// TestRunTrustPromptAccept covers the four accept keys.
func TestRunTrustPromptAccept(t *testing.T) {
	for _, key := range []byte{'y', 'Y', '\r', '\n', ' ', '1'} {
		got, err := runTrustPrompt("/tmp", bytes.NewReader([]byte{key}), io.Discard)
		if err != nil {
			t.Fatalf("key=%q: %v", key, err)
		}
		if got != trustAccept {
			t.Errorf("key=%q: got %v, want trustAccept", key, got)
		}
	}
}

// TestRunTrustPromptDeny covers the deny keys + EOF.
func TestRunTrustPromptDeny(t *testing.T) {
	for name, in := range map[string]*bytes.Reader{
		"n":      bytes.NewReader([]byte{'n'}),
		"N":      bytes.NewReader([]byte{'N'}),
		"q":      bytes.NewReader([]byte{'q'}),
		"Ctrl-C": bytes.NewReader([]byte{0x03}),
		"Esc":    bytes.NewReader([]byte{0x1b}),
		"x":      bytes.NewReader([]byte{'x'}),
		"EOF":    bytes.NewReader(nil),
	} {
		got, err := runTrustPrompt("/tmp", in, io.Discard)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != trustDeny {
			t.Errorf("%s: got %v, want trustDeny", name, got)
		}
	}
}

// TestTrustGateFlagSkip covers the --trust-cwd test hook.
func TestTrustGateFlagSkip(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Even with NO settings.json the flag must skip the dialog.
	fs := newFlagSetForTrustGate()
	fs.Set("trust-cwd", "true")
	cwd, skip, err := trustGate(fs)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if !skip {
		t.Errorf("trust-cwd flag should skip the dialog")
	}
	if !strings.HasSuffix(cwd, dir) && !strings.HasSuffix(cwd, filepath.Base(dir)) {
		t.Errorf("cwd should end with %q; got %q", dir, cwd)
	}
}

// TestTrustGateEnvVar covers the OPENAGY_TRUST=test hook.
func TestTrustGateEnvVar(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// OPENAGY_TRUST=1 always skips.
	t.Setenv("OPENAGY_TRUST", "1")
	cwd, skip, err := trustGate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if !skip {
		t.Errorf("OPENAGY_TRUST=1 should skip the dialog")
	}
	if cwd == "" {
		t.Errorf("cwd should be populated even when skipping")
	}

	// OPENAGY_TRUST=<tmp> matches cwd's parent.
	t.Setenv("OPENAGY_TRUST", filepath.Dir(dir))
	cwd, skip, err = trustGate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if !skip {
		t.Errorf("OPENAGY_TRUST=<parent of cwd> should skip; cwd=%q env=%q", cwd, filepath.Dir(dir))
	}

	// OPENAGY_TRUST=/some/other/path does NOT skip.
	t.Setenv("OPENAGY_TRUST", "/some/other/path")
	_, skip, err = trustGate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if skip {
		t.Errorf("OPENAGY_TRUST to an unrelated path must not skip")
	}
}

// TestTrustGateSettingsTrusted covers the normal cycle: cwd is in
// settings.json → no prompt.
func TestTrustGateSettingsTrusted(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Inject a settings.json that trusts dir.
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSandboxSettings(t, home, `{"trustedWorkspaces":["`+dir+`"]}`)

	cwd, skip, err := trustGate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if !skip {
		t.Errorf("cwd already in settings.json should skip; cwd=%q", cwd)
	}
}

// TestTrustGateSettingsMissing covers the first-launch path: no
// settings.json at all → gate does NOT skip, prompt must be shown.
func TestTrustGateSettingsMissing(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir()) // empty HOME — no settings.json

	_, skip, err := trustGate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if skip {
		t.Errorf("missing settings.json must not skip — the prompt is the whole point")
	}
}

// TestTrustPromptAcceptPersists exercises the full first-launch flow:
// no settings.json on disk → trust gate says "show the prompt" →
// "y" is read → cwd ends up in trustedWorkspaces. Uses the real
// addTrustedWorkspace wiring via the init() in main.go so this
// catches a regression where the trust prompt and persistence get
// out of sync (e.g. cycle 125 wired one but not the other).
func TestTrustPromptAcceptPersists(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	// Confirm pre-condition: gate says "show the prompt".
	if _, skip, _ := trustGate(nil); skip {
		t.Fatalf("precondition: empty settings.json should not skip the prompt")
	}

	// Drive runTrustPrompt with a 'y' on stdin.
	res, err := runTrustPrompt(dir, bytes.NewReader([]byte{'y'}), io.Discard)
	if err != nil {
		t.Fatalf("runTrustPrompt: %v", err)
	}
	if res != trustAccept {
		t.Fatalf("runTrustPrompt returned %v, want trustAccept", res)
	}

	// The same persistence call the runLiveTUI makes after a yes.
	if err := addTrustedWorkspace(dir); err != nil {
		t.Fatalf("addTrustedWorkspace: %v", err)
	}

	// Post-condition: settings.json now lists cwd as trusted.
	body, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json should exist after accept; read: %v", err)
	}
	if !strings.Contains(string(body), dir) {
		t.Errorf("settings.json should contain %q; got:\n%s", dir, body)
	}

	// And the gate now says "skip".
	if _, skip, _ := trustGate(nil); !skip {
		t.Errorf("after accept the gate must skip on the next launch")
	}
}

// ---- helpers ------------------------------------------------------------

// chdir changes the test's working directory for the duration of the
// test (t.Cleanup restores). Required because trustGate calls
// os.Getwd() — without chdir, every test sees the same cwd as the
// parent process and the assertions on dir are meaningless.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir(%s): %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// writeSandboxSettings writes a settings.json under $ANTIGRAVITY_HOME
// so trustGate's IsWorkspaceTrusted call sees a sandbox-only list
// rather than the real ~/.gemini/antigravity-cli/settings.json
// (the production path runs against the user's HOME, but
// auth.AntigravityConfigHome honours $ANTIGRAVITY_HOME first — when
// set it returns $ANTIGRAVITY_HOME verbatim, see
// internal/auth/compat.go).
func writeSandboxSettings(t *testing.T, home, body string) {
	t.Helper()
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(body), 0600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
}

// newFlagSetForTrustGate returns a *flag.FlagSet with a single
// --trust-cwd flag wired in the way main.go registers it. Keeps the
// test independent of the production flag parser.
func newFlagSetForTrustGate() *flag.FlagSet {
	fs := flag.NewFlagSet("trustgate-test", flag.ContinueOnError)
	fs.Bool("trust-cwd", false, "skip the on-startup workspace trust dialog")
	return fs
}
