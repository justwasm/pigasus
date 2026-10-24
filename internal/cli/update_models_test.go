// Tests for the `update` and `models` subcommands.
//
// update: the v1.3.5 binary is a real self-updater — it queries
// https://antigravity-cli-auto-updater-974169037036.us-central1.run.app,
// and when a newer version exists it downloads and stages a replacement
// binary ("✅ Found new version X." then "⟳ Downloading update...").
// The recovered clone deliberately does not perform network I/O or
// self-replace; it prints the deterministic "already on the latest"
// result. This is the one intentional behavioural divergence and is
// documented in the deliverable report. The tests below pin that
// deterministic output so any accidental change is caught.
//
// models: the model catalogue is server-driven. Signed out (a fresh
// $HOME), the reference prints "Fetching available models..." then
// "Error: Please sign in ..." on stderr and exits 1. Signed in, it prints
// the catalogue on stdout. We reproduce the signed-out path exactly and
// emit a representative catalogue when signed in.
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// runCapture runs args through the standard parser and returns
// (stdout, stderr, exitCode).
func runCapture(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	p := NewParser(StandardFlags(), StandardSubcommands(), "antigravity", "recovered", "recovered")
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	_ = p.ExecuteArgs(args)
	return out.String(), errb.String(), p.ExitCode()
}

func TestUpdate_Offline(t *testing.T) {
	out, _, code := runCapture(t, "update")
	want := "\u27f3 Checking for updates... (current version " + ReferenceVersion + ")\n" +
		"\u2713 You are already on the latest version.\n"
	if out != want {
		t.Errorf("update stdout mismatch\n got: %q\nwant: %q", out, want)
	}
	if code != 0 {
		t.Errorf("update exit code: got %d, want 0", code)
	}
}

func TestUpdate_NoStubLiteral(t *testing.T) {
	out, _, _ := runCapture(t, "update")
	if strings.Contains(out, "[recovered]") || strings.Contains(out, "not implemented") {
		t.Errorf("update still emits a cycle-38 stub literal\n--- got ---\n%s", out)
	}
}

func TestModels_SignedOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no token files → signed out
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	out, errb, code := runCapture(t, "models")
	if out != "" {
		t.Errorf("models stdout should be empty when signed out, got %q", out)
	}
	wantErr := "Fetching available models...\n" +
		"Error: Please sign in to view available models. Launch the CLI without arguments to sign in.\n"
	if errb != wantErr {
		t.Errorf("models stderr mismatch\n got: %q\nwant: %q", errb, wantErr)
	}
	if code != 1 {
		t.Errorf("models exit code: got %d, want 1", code)
	}
}

func TestModels_SignedIn(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	out, errb, code := runCapture(t, "models")
	if code != 0 {
		t.Fatalf("models exit code: got %d, want 0 (stderr=%q)", code, errb)
	}
	// 【一手】 The fetch banner is on stderr; stdout carries only the rows.
	if errb != "Fetching available models...\n" {
		t.Errorf("models stderr should be the fetch banner, got:\n%s", errb)
	}
	if strings.HasPrefix(out, "Fetching available models...") {
		t.Errorf("models stdout should not carry the fetch banner, got:\n%s", out)
	}
	// Every line after the banner is "<id>\t<label>".
	for _, want := range []string{
		"gemini-3.8-flash-high",
		"gemini-3.8-flash-medium",
		"gemini-3.8-flash-low",
		"claude-sonnet-4-6",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("models output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestModels_ExtraArgsRejected(t *testing.T) {
	out, errb, code := runCapture(t, "models", "extra")
	if out != "" {
		t.Errorf("models extra stdout should be empty, got %q", out)
	}
	if errb != "Error: unexpected arguments: [extra]\n" {
		t.Errorf("models extra stderr mismatch: %q", errb)
	}
	if code != 1 {
		t.Errorf("models extra exit code: got %d, want 1", code)
	}
}
