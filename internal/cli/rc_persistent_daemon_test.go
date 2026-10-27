// Tests for the cycle rc-persistent-daemon start|status|stop +
// hidden foreground bridge.  Each handler has its own help-only
// test; the binding+launchd wiring is covered inside
// internal/persistentrc/persistentrc_test.go via Spec.SkipLaunchd
// so the broader cli test package doesn't depend on launchd.
//
// References:
//   - close_gap_test.go / TestRemoteControlStatus_1to1 — pins the
//     JSON shape the brief requires.
//   - internal/persistentrc/ TestStartStop_DryRun — pins the
//     handler-Spec start/stop pair without launchd.
package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestRemoteControlStart_HelpOnly pins the help text the parser
// emits on `wagyu remote-control start --help`.
//
// The usage block is the reference's, byte for byte: the reference
// advertises `--name` and `--session` only. The clone also accepts
// `--addr` and `--log-file` (the foreground daemon's overrides and
// the persistent daemon's log path), but the reference does not list
// them — `agy remote-control start --addr 1.2.3.4:9` prints this very
// usage block — so the clone does not either.
//
// `--desktop` follows the same rule: accepted, not advertised.
func TestRemoteControlStart_HelpOnly(t *testing.T) {
	out, errb, code := runJetski(t, "remote-control", "start", "--help")
	if code != 0 {
		t.Errorf("start --help exit code = %d, want 0; stderr=%q", code, errb)
	}
	if out != "" {
		t.Errorf("start --help stdout should be empty, got %q", out)
	}
	for _, flag := range []string{"--name", "--session"} {
		if !strings.Contains(errb, flag) {
			t.Errorf("start --help usage missing %q; stderr=%q", flag, errb)
		}
	}
	for _, flag := range []string{"--addr", "--log-file"} {
		if strings.Contains(errb, flag) {
			t.Errorf("start --help usage must not advertise %q (the reference does not); stderr=%q", flag, errb)
		}
	}
	if strings.Contains(errb, "--desktop") {
		t.Errorf("start --help usage must not advertise --desktop (the reference does not); stderr=%q", errb)
	}
}

// TestRemoteControlStart_DesktopAccepted pins that the parser
// accepts the desktop switch on `remote-control start`.
//
// The full flow (the LaunchAgent plist carrying `--desktop`) is
// pinned in internal/persistentrc; here the parser surface is what
// is under test. The flag is *not* advertised in the usage block —
// the reference's `start` block is byte for byte, and the clone
// follows the same rule it follows for `--addr` and `--log-file`.
func TestRemoteControlStart_DesktopAccepted(t *testing.T) {
	j := &JetskiCLI{
		Name:   "pigasus",
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	}
	opts, err := parseRCStartFlags(j, []string{"--desktop"}, usageRCStart("pigasus"))
	if err != nil {
		t.Fatalf("parseRCStartFlags(--desktop): %v", err)
	}
	if !opts.desktop {
		t.Errorf("--desktop did not reach the parser")
	}
	opts, err = parseRCStartFlags(j, []string{"--name", "x"}, usageRCStart("pigasus"))
	if err != nil {
		t.Fatalf("parseRCStartFlags(--name x): %v", err)
	}
	if opts.desktop {
		t.Errorf("desktop = true without --desktop")
	}
}

// TestRemoteControlStop_HelpOnly pins the help text the parser
// emits on `wagyu remote-control stop --help`. The reference's block
// lists the two help flags only.
func TestRemoteControlStop_HelpOnly(t *testing.T) {
	out, errb, code := runJetski(t, "remote-control", "stop", "--help")
	if code != 0 {
		t.Errorf("stop --help exit code = %d, want 0; stderr=%q", code, errb)
	}
	if out != "" {
		t.Errorf("stop --help stdout should be empty, got %q", out)
	}
	if strings.Contains(errb, "--name") {
		t.Errorf("stop --help usage must not advertise --name (the reference does not); stderr=%q", errb)
	}
}

// TestRemoteControlStatus_JSONOutput pins the brief's contract:
// `wagyu remote-control status` (no args) emits a JSON document.
// The document's "running" field is false on a fresh machine —
// no LaunchAgent registered; "name" defaults to hostname;
// "hint" surfaces the operator's next step.
func TestRemoteControlStatus_JSONOutput(t *testing.T) {
	out, errb, code := runJetski(t, "remote-control", "status", "--json")
	if code != 0 {
		t.Fatalf("status exit code = %d, want 0; stderr=%q", code, errb)
	}
	if errb != "" {
		t.Errorf("status stderr should be empty, got %q", errb)
	}
	// JSON decode is already covered by
	// TestRemoteControlStatus_1to1; this test ensures the
	// parser wires through to the runRCStatus handler, not
	// the stub.
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("status output is not a JSON document; got=%q", out)
	}
	if !strings.Contains(out, `"running": false`) {
		t.Errorf("status JSON missing running=false; got=%q", out)
	}
}

// TestRemoteControlStatus_NameOverride: --name lets the operator
// query a daemon whose label is different from the local
// hostname.  We can't rely on a real daemon being registered;
// instead we just verify the parser routes the name through to
// the JSON document.
func TestRemoteControlStatus_NameOverride(t *testing.T) {
	out, errb, code := runJetski(t,
		"remote-control", "status", "--json", "--name", "test-machine")
	if code != 0 {
		t.Fatalf("status --name exit code = %d, want 0; stderr=%q", code, errb)
	}
	if errb != "" {
		t.Errorf("status --name stderr should be empty, got %q", errb)
	}
	if !strings.Contains(out, `"name": "test-machine"`) {
		t.Errorf("status JSON name was not threaded through; got=%q", out)
	}

	outPlain, _, _ := runJetski(t,
		"remote-control", "status", "--name", "test-machine")
	if !strings.Contains(outPlain, "Instance name: test-machine") {
		t.Errorf("status plaintext name was not threaded through; got=%q", outPlain)
	}
}

// TestRemoteControlStart_RejectsUnknownFlag: the parser rejects
// flags that aren't --addr/--log-file/--name/--session with a
// non-zero exit and a stderr usage message.
func TestRemoteControlStart_RejectsUnknownFlag(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "start", "--bogus")
	if code != 1 {
		t.Errorf("start --bogus exit code = %d, want 1", code)
	}
	if !strings.Contains(errb, "-bogus") {
		t.Errorf("start --bogus stderr should mention the flag; got=%q", errb)
	}
}

// TestRemoteControlStart_RequiresArgAfterAddr: --addr without a
// value is rejected as a usage error.
func TestRemoteControlStart_RequiresArgAfterAddr(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "start", "--addr")
	if code != 1 {
		t.Errorf("start --addr (no value) exit code = %d, want 1", code)
	}
	if !strings.Contains(errb, "--addr") {
		t.Errorf("start --addr (no value) stderr should mention --addr; got=%q", errb)
	}
}

// TestRemoteControlForeground_Hidden pins the brief's
// requirement that the foreground subcommand not appear in
// `wagyu --help`.  The subcommand is reachable via direct
// invocation; the parser_specs.go spec sets Hidden: true.
func TestRemoteControlForeground_Hidden(t *testing.T) {
	_, errb, code := runJetski(t, "--help")
	if code != 0 {
		t.Fatalf("--help exit code = %d, want 0; stderr=%q", code, errb)
	}
	if strings.Contains(errb, "remote-control-foreground") {
		t.Errorf("--help output should NOT mention remote-control-foreground; got=%q", errb)
	}
}

// TestRemoteControlForeground_DirectInvocation: when invoked
// directly, the hidden subcommand routes through
// runRCForegroundPlaceholder → the registered handler (a
// test-installed no-op).
func TestRemoteControlForeground_DirectInvocation(t *testing.T) {
	orig := foregroundHandler
	t.Cleanup(func() { foregroundHandler = orig })

	got := []string{}
	foregroundHandler = func(j *JetskiCLI, args []string) error {
		got = append(got, args...)
		return nil
	}

	_, _, code := runJetski(t, "remote-control-foreground", "--addr", "0.0.0.0:8787", "--name", "x")
	if code != 0 {
		t.Errorf("remote-control-foreground exit code = %d, want 0", code)
	}
	if len(got) < 4 || got[0] != "--addr" {
		t.Errorf("foreground handler args = %v; want [--addr 0.0.0.0:8787 --name x]", got)
	}
}

// TestRemoteControlStart_HostnameDefault: when --name is unset,
// runRCStart should use os.Hostname().  We can't drive the full
// Start flow without launchd, so we pin the hostname-default
// path by inspecting the JSON output of `status` — which
// resolves --name to hostname when the user didn't pass one.
func TestRemoteControlStart_HostnameDefault(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	hn, err := os.Hostname()
	if err != nil || hn == "" {
		t.Skipf("no hostname; skipping (err=%v)", err)
	}
	out, _, code := runJetski(t, "remote-control", "status", "--json")
	if code != 0 {
		t.Fatalf("status exit = %d", code)
	}
	want := `"name": "` + hn + `"`
	if !strings.Contains(out, want) {
		t.Errorf("status JSON default name = %q; want substring %q", out, want)
	}
}
