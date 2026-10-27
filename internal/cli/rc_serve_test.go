// Tests for the cycle rc-serve-subcommand parser surface.
//
// Coverage:
//
//   - help text shows the same flag list the brief pins
//     (--addr / --name / --no-login) and stays out of
//     `wagyu --help` (Hidden: false, vs. the hidden
//     `remote-control-foreground` subcommand).
//   - the parent `remote-control` usage block lists serve
//     alongside start/status/stop.
//   - the parser routes `remote-control serve --help` to
//     runRCServe without spawning the bridge.
//
// The test rig (cli-internal) does not install the bridge
// handler, so the "no handler registered" stderr notice is
// the success sentinel for the parser routing tests below.
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// serveHandlerRegistered reports whether the cmd/wagyu bridge has been
// wired into the parser. Cycle 346: this seam used to live in
// production code (cli.EnsureServeHandlerRegistered) and was used only
// by the tests below; it is a test concern, so it lives here now.
func serveHandlerRegistered() bool { return serveHandler != nil }

// TestRemoteControlServe_HelpOnly pins the help text the
// parser emits on `wagyu remote-control serve --help`.
// Cycle rc-serve-subcommand added --addr, --name,
// --no-login; the test fails immediately if any of them
// drops out of the usage block.
func TestRemoteControlServe_HelpOnly(t *testing.T) {
	out, errb, code := runJetski(t, "remote-control", "serve", "--help")
	if code != 0 {
		t.Errorf("serve --help exit code = %d, want 0; stderr=%q", code, errb)
	}
	if out != "" {
		t.Errorf("serve --help stdout should be empty, got %q", out)
	}
	for _, flag := range []string{"--addr", "--name", "--no-login"} {
		if !strings.Contains(errb, flag) {
			t.Errorf("serve --help usage missing %q; stderr=%q", flag, errb)
		}
	}
	// Brief usage wording: must include "foreground" so a
	// dashboard grep for the term distinguishes serve from
	// start.
	if !strings.Contains(errb, "foreground") {
		t.Errorf("serve --help usage missing 'foreground' wording; stderr=%q", errb)
	}
}

// TestRemoteControlServe_HiddenInParentUsage pins the reference's
// 1.4.6 listing: the parent `remote-control` usage block shows
// start, status and stop only — `serve` exists in the reference (it
// starts the foreground daemon) but is not advertised.
//
//	$ agy remote-control --help
//	Usage: agy remote-control <subcommand>
//
//	Available subcommands:
//	  start    Register and start the remote-control daemon
//	  status   Show the daemon's status
//	  stop     Stop the daemon and unregister it
func TestRemoteControlServe_HiddenInParentUsage(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "--help")
	if code != 0 {
		t.Fatalf("remote-control --help exit code = %d, want 0; stderr=%q", code, errb)
	}
	if strings.Contains(errb, "serve") {
		t.Errorf("remote-control --help must not list 'serve' (the reference does not); stderr=%q", errb)
	}
	for _, sub := range []string{"start", "status", "stop"} {
		if !strings.Contains(errb, sub) {
			t.Errorf("remote-control --help missing %q; stderr=%q", sub, errb)
		}
	}
}

// TestRemoteControlServe_NoBridgeNotice: when invoked
// without the bridge (the cli-internal test rig doesn't
// install cli.RegisterServeHandler), the parser routes the
// argv to runRCServe, which prints "no handler registered"
// and exits 0. This pins the bridge-not-installed path so
// a future refactor that removes the notice fails here.
func TestRemoteControlServe_NoBridgeNotice(t *testing.T) {
	// Defensive: the cli-internal rig never installs the
	// bridge (cmd/wagyu/main.go does). If a future test
	// forgets that contract and installs one, this test
	// becomes meaningless; skip rather than fail.
	if serveHandlerRegistered() {
		t.Skip("serve handler is registered; the no-bridge notice is not testable")
	}
	out, errb, code := runJetski(t, "remote-control", "serve", "--no-login")
	if code != 0 {
		t.Fatalf("serve --no-login exit code = %d, want 0; stderr=%q", code, errb)
	}
	if !strings.Contains(errb, "no handler registered") {
		t.Errorf("serve stderr should mention 'no handler registered'; got=%q", errb)
	}
	// In the no-bridge path runRCServe short-circuits
	// BEFORE printing the "Remote-control daemon stopped."
	// banner — that line is emitted only when the bridge
	// actually ran. The full sentinel is exercised in the
	// cmd/wagyu integration tests via the end-to-end
	// shutdown path. Here we just confirm stdout is empty.
	if out != "" {
		t.Errorf("serve (no bridge) stdout should be empty, got=%q", out)
	}
}

// TestRemoteControlServe_RejectsUnknownFlag pins the
// parser-level rejection of flags runRCServe doesn't
// understand. Mirrors the start / status / stop coverage in
// rc_persistent_daemon_test.go.
func TestRemoteControlServe_RejectsUnknownFlag(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "serve", "--bogus")
	if code != 1 {
		t.Errorf("serve --bogus exit code = %d, want 1", code)
	}
	if !strings.Contains(errb, "-bogus") {
		t.Errorf("serve --bogus stderr should mention the flag; got=%q", errb)
	}
}

// TestRemoteControlServe_RequiresArgAfterAddr: --addr
// without a following value is a usage error (mirrors
// runRCStart's TestRemoteControlStart_RequiresArgAfterAddr).
func TestRemoteControlServe_RequiresArgAfterAddr(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "serve", "--addr")
	if code != 1 {
		t.Errorf("serve --addr (no value) exit code = %d, want 1", code)
	}
	if !strings.Contains(errb, "--addr") {
		t.Errorf("serve --addr (no value) stderr should mention --addr; got=%q", errb)
	}
}

// TestRemoteControlServe_HostnameDefault: --name falls
// back to os.Hostname() when unset, the same way
// runRCStart's flag-defaulting does.
func TestRemoteControlServe_HostnameDefault(t *testing.T) {
	// The cli-internal rig routes through runRCServe →
	// bridge (nil) → "no handler registered" stderr
	// notice. We can't observe the name the bridge
	// receives (the bridge is what prints the banner),
	// so we pin the parser path instead: --name must
	// not be required.
	if serveHandlerRegistered() {
		t.Skip("serve handler is registered; the hostname-default path is exercised end-to-end in cmd/wagyu/")
	}
	_, _, code := runJetski(t, "remote-control", "serve", "--no-login")
	if code != 0 {
		t.Errorf("serve (no --name) exit code = %d, want 0", code)
	}
}

// TestRemoteControlServe_NoLoginAccepted pins the brief:
// --no-login is accepted by the parser (no usage error).
// The flag's behaviour (skip OAuth device flow) is exercised
// in the cmd/wagyu integration test rig.
func TestRemoteControlServe_NoLoginAccepted(t *testing.T) {
	if serveHandlerRegistered() {
		t.Skip("serve handler is registered; --no-login behaviour is exercised in cmd/wagyu/")
	}
	_, errb, code := runJetski(t, "remote-control", "serve", "--no-login")
	if code != 0 {
		t.Errorf("serve --no-login exit code = %d, want 0; stderr=%q", code, errb)
	}
	if strings.Contains(errb, "--no-login") {
		t.Errorf("serve --no-login stderr should not mention --no-login (flag is accepted); got=%q", errb)
	}
}

// TestRemoteControlServe_DesktopAccepted pins the remote-control
// twin of `pigasus web --desktop`: the switch is accepted by the
// parser (no usage error) and it is advertised in the usage block.
//
// The flag's behaviour (the hub's Mendel identity) is exercised in
// internal/rchost.
func TestRemoteControlServe_DesktopAccepted(t *testing.T) {
	if serveHandlerRegistered() {
		t.Skip("serve handler is registered; --desktop behaviour is exercised in internal/rchost")
	}
	_, errb, code := runJetski(t, "remote-control", "serve", "--desktop")
	if code != 0 {
		t.Errorf("serve --desktop exit code = %d, want 0; stderr=%q", code, errb)
	}
	if strings.Contains(errb, "--desktop") {
		t.Errorf("serve --desktop stderr should not mention --desktop (flag is accepted); got=%q", errb)
	}
}

// TestRemoteControlServe_DesktopInUsage pins that the switch is
// discoverable: the usage block lists it.
func TestRemoteControlServe_DesktopInUsage(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "serve", "--help")
	if code != 0 {
		t.Fatalf("serve --help exit code = %d, want 0; stderr=%q", code, errb)
	}
	if !strings.Contains(errb, "--desktop") {
		t.Errorf("serve --help usage missing --desktop; stderr=%q", errb)
	}
}

// TestRemoteControlServe_DesktopReachesTheBridge pins the whole
// parser→bridge hop: the switch travels from the argv into the
// typed ServeArgs the handler body reads.
//
// It is the remote-control twin of TestWebSubcommand_DesktopFlag —
// 【一手】 measured 2026-10-27: the hub's identity gets the desktop
// flag set, the CLI's does not. See internal/auth's package comment.
func TestRemoteControlServe_DesktopReachesTheBridge(t *testing.T) {
	var captured ServeArgs
	RegisterServeHandler(func(j *JetskiCLI, args ServeArgs) error {
		captured = args
		return nil
	})
	t.Cleanup(func() { RegisterServeHandler(nil) })

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{
		Name:   "pigasus",
		Out:    out,
		ErrOut: errOut,
	}

	if err := runRCServe(j, []string{"--desktop"}); err != nil {
		t.Fatalf("runRCServe(--desktop) failed: %v", err)
	}
	if !captured.Desktop {
		t.Errorf("expected Desktop=true")
	}

	if err := runRCServe(j, []string{}); err != nil {
		t.Fatalf("runRCServe failed: %v", err)
	}
	if captured.Desktop {
		t.Errorf("expected Desktop=false without the flag")
	}
}
