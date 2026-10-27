// Tests for the v1.3.5 subcommand parity surface (the "close-gap" set).
//
// Reference behaviour (verified against the v1.3.5 binary; argv0 basename
// used in every message):
//
//	agy version                 → stderr error, exit 2
//	agy plugin list             → stdout "No plugins installed." when no
//	                              plugins are installed, exit 0
//	agy remote-control status   → stdout 2-line daemon status, exit 0
//	agy agents list             → stderr error, exit 1
//
// Errors/usage go to stderr; normal results go to stdout. These tests
// capture both streams and assert the exit code (via Parser.ExitCode).
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/persistentrc"
)

// runJetski executes one invocation through the standard v1.3.5 parser and
// returns (stdout, stderr, exitCode).
func runJetski(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	p := NewParser(StandardFlags(), StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	_ = p.ExecuteArgs(args)
	return out.String(), errb.String(), p.ExitCode()
}

func TestVersion_1to1(t *testing.T) {
	out, errb, code := runJetski(t, "version")
	if out != "" {
		t.Errorf("agy version stdout should be empty, got %q", out)
	}
	want := "Error: unexpected argument \"version\".\n" +
		"Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.\n"
	if errb != want {
		t.Errorf("agy version stderr mismatch:\n got: %q\nwant: %q", errb, want)
	}
	if code != 2 {
		t.Errorf("agy version should exit 2, got %d", code)
	}
}

func TestPluginList_1to1(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no plugins installed
	out, errb, code := runJetski(t, "plugin", "list")
	want := "No plugins installed.\n"
	if out != want {
		t.Errorf("agy plugin list stdout mismatch:\n got: %q\nwant: %q", out, want)
	}
	if errb != "" {
		t.Errorf("agy plugin list stderr should be empty, got %q", errb)
	}
	if code != 0 {
		t.Errorf("agy plugin list should exit 0, got %d", code)
	}
}

func TestRemoteControlStatus_1to1(t *testing.T) {
	out, errb, code := runJetski(t, "remote-control", "status", "--json")
	if errb != "" {
		t.Errorf("agy remote-control status stderr should be empty, got %q", errb)
	}
	if code != 0 {
		t.Errorf("agy remote-control status should exit 0, got %d", code)
	}
	// Cycle rc-persistent-daemon: status --json emits a JSON document.
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("agy remote-control status stdout should end with newline, got %q", out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("agy remote-control status stdout is not valid JSON: %v\nout=%q", err, out)
	}
	wantName := persistentrc.ResolveInstanceName("")
	if v, ok := got["name"]; !ok {
		t.Errorf("JSON missing `name` field; got=%v", got)
	} else if wantName != "" && v.(string) != wantName {
		t.Errorf("JSON name = %q, want %q", v, wantName)
	}
}

func TestRemoteControlStatus_Plaintext(t *testing.T) {
	out, errb, code := runJetski(t, "remote-control", "status")
	if errb != "" {
		t.Errorf("status stderr should be empty, got %q", errb)
	}
	if code != 0 {
		t.Errorf("status exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "Instance name:") {
		t.Errorf("status plaintext should contain 'Instance name:', got %q", out)
	}
	if !strings.Contains(out, endpoints.Current().CompanionOrigin) {
		t.Errorf("status plaintext should mention the companion origin, got %q", out)
	}
}

func TestAgentsList_1to1(t *testing.T) {
	out, errb, code := runJetski(t, "agents", "list")
	if out != "" {
		t.Errorf("agy agents list stdout should be empty, got %q", out)
	}
	want := "Error: unexpected arguments: [list]\n"
	if errb != want {
		t.Errorf("agy agents list stderr mismatch:\n got: %q\nwant: %q", errb, want)
	}
	if code != 1 {
		t.Errorf("agy agents list should exit 1, got %d", code)
	}
}

func TestLogoHiddenSubcommand(t *testing.T) {
	orig := logoHandler
	t.Cleanup(func() { logoHandler = orig })

	called := false
	RegisterLogoHandler(func(w io.Writer, args []string) error {
		called = true
		w.Write([]byte("custom logo\n"))
		return nil
	})

	out, errb, code := runJetski(t, "logo")
	if !called {
		t.Errorf("expected logoHandler to be called")
	}
	if out != "custom logo\n" {
		t.Errorf("stdout got %q, want %q", out, "custom logo\n")
	}
	if errb != "" {
		t.Errorf("stderr got %q, want empty", errb)
	}
	if code != 0 {
		t.Errorf("exit code got %d, want 0", code)
	}
}

func TestLogoSubcommand_Help(t *testing.T) {
	out, errb, code := runJetski(t, "logo", "--help")
	if code != 0 {
		t.Errorf("exit code got %d, want 0", code)
	}
	want := UsageLogo("antigravity")
	if errb != want {
		t.Errorf("stderr mismatch:\n got: %q\nwant: %q", errb, want)
	}
	if out != "" {
		t.Errorf("stdout got %q, want empty", out)
	}
}

// TestParentNoArgsUsage pins the parent-command no-arg behaviour: `mcp`
// and `remote-control` print their usage block to stderr and exit 0.
func TestParentNoArgsUsage(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want string
	}{
		{"mcp", usageMCP("antigravity")},
		{"remote-control", usageRemoteControl("antigravity")},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			out, errb, code := runJetski(t, tc.cmd)
			if out != "" {
				t.Errorf("%s: stdout should be empty, got %q", tc.cmd, out)
			}
			if errb != tc.want {
				t.Errorf("%s: stderr mismatch\n got: %q\nwant: %q", tc.cmd, errb, tc.want)
			}
			if code != 0 {
				t.Errorf("%s: exit code got %d, want 0", tc.cmd, code)
			}
		})
	}
}
