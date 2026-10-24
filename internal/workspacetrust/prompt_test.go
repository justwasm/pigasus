// Package workspacetrust_test tests the workspace-trust gate.
package workspacetrust_test

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/workspacetrust"
)

func TestRenderTrustPromptSmoke(t *testing.T) {
	var buf bytes.Buffer
	cwd := "/Users/gear/GitHub/openagy"
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cwd = home + "/GitHub/openagy"
	}
	if err := workspacetrust.RenderTrustPrompt(cwd, &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	want := []string{
		"Trust this folder?",
		"Do you trust the contents of this project?",
		"Yes, I trust this folder",
		"~/GitHub/openagy",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("render output missing %q\nfull output:\n%s", w, out)
		}
	}
}

func TestRenderTrustPromptRawPath(t *testing.T) {
	var buf bytes.Buffer
	if err := workspacetrust.RenderTrustPrompt("/tmp/agytui-probe", &buf); err != nil {
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

func TestRunTrustPromptAccept(t *testing.T) {
	for _, key := range []byte{'y', 'Y', '\r', '\n', ' ', '1'} {
		got, err := workspacetrust.RunTrustPrompt("/tmp", bytes.NewReader([]byte{key}), io.Discard)
		if err != nil {
			t.Fatalf("key=%q: %v", key, err)
		}
		if got != workspacetrust.Accept {
			t.Errorf("key=%q: got %v, want Accept", key, got)
		}
	}
}

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
		got, err := workspacetrust.RunTrustPrompt("/tmp", in, io.Discard)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != workspacetrust.Deny {
			t.Errorf("%s: got %v, want Deny", name, got)
		}
	}
}

func TestTrustGateFlagSkip(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	fs := newFlagSetForTrustGate()
	fs.Set("trust-cwd", "true")
	cwd, skip, err := workspacetrust.Gate(fs)
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

func TestTrustGateEnvVar(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	t.Setenv("OPENAGY_TRUST", "1")
	cwd, skip, err := workspacetrust.Gate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if !skip {
		t.Errorf("OPENAGY_TRUST=1 should skip the dialog")
	}
	if cwd == "" {
		t.Errorf("cwd should be populated even when skipping")
	}

	t.Setenv("OPENAGY_TRUST", filepath.Dir(dir))
	cwd, skip, err = workspacetrust.Gate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if !skip {
		t.Errorf("OPENAGY_TRUST=<parent of cwd> should skip; cwd=%q env=%q", cwd, filepath.Dir(dir))
	}

	t.Setenv("OPENAGY_TRUST", "/some/other/path")
	_, skip, err = workspacetrust.Gate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if skip {
		t.Errorf("OPENAGY_TRUST to an unrelated path must not skip")
	}
}

func TestTrustGateSettingsTrusted(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSandboxSettings(t, home, `{"trustedWorkspaces":["`+dir+`"]}`)

	cwd, skip, err := workspacetrust.Gate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if !skip {
		t.Errorf("cwd already in settings.json should skip; cwd=%q", cwd)
	}
}

func TestTrustGateSettingsMissing(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	_, skip, err := workspacetrust.Gate(nil)
	if err != nil {
		t.Fatalf("trustGate: %v", err)
	}
	if skip {
		t.Errorf("missing settings.json must not skip")
	}
}

func TestTrustPromptAcceptPersists(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	if _, skip, _ := workspacetrust.Gate(nil); skip {
		t.Fatalf("precondition: empty settings.json should not skip the prompt")
	}

	res, err := workspacetrust.RunTrustPrompt(dir, bytes.NewReader([]byte{'y'}), io.Discard)
	if err != nil {
		t.Fatalf("runTrustPrompt: %v", err)
	}
	if res != workspacetrust.Accept {
		t.Fatalf("runTrustPrompt returned %v, want Accept", res)
	}

	if err := workspacetrust.AddTrustedWorkspace(dir); err != nil {
		t.Fatalf("addTrustedWorkspace: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json should exist after accept; read: %v", err)
	}
	if !strings.Contains(string(body), dir) {
		t.Errorf("settings.json should contain %q; got:\n%s", dir, body)
	}

	if _, skip, _ := workspacetrust.Gate(nil); !skip {
		t.Errorf("after accept the gate must skip on the next launch")
	}
}

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

func writeSandboxSettings(t *testing.T, home, body string) {
	t.Helper()
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(body), 0600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
}

func newFlagSetForTrustGate() *flag.FlagSet {
	fs := flag.NewFlagSet("trustgate-test", flag.ContinueOnError)
	fs.Bool("trust-cwd", false, "skip the on-startup workspace trust dialog")
	return fs
}
