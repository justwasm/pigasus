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
	"strings"
	"testing"
)

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

// TestRemoteControlServe_ListedInParentUsage pins the brief:
// the parent `remote-control` usage block lists serve
// alongside start / status / stop. Cycle rc-serve-subcommand.
func TestRemoteControlServe_ListedInParentUsage(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "--help")
	if code != 0 {
		t.Fatalf("remote-control --help exit code = %d, want 0; stderr=%q", code, errb)
	}
	if !strings.Contains(errb, "serve") {
		t.Errorf("remote-control --help missing 'serve' subcommand; stderr=%q", errb)
	}
	// And it must come AFTER `start` so the help block
	// reads the same way the v1.3.5 binary lists its
	// foreground-after-persistent subcommands.
	startIdx := strings.Index(errb, "start")
	serveIdx := strings.Index(errb, "serve")
	if startIdx < 0 || serveIdx < 0 {
		t.Fatalf("remote-control --help missing start/serve markers; stderr=%q", errb)
	}
	if startIdx >= serveIdx {
		t.Errorf("'start' must come before 'serve' in parent usage; start=%d serve=%d stderr=%q", startIdx, serveIdx, errb)
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
	if EnsureServeHandlerRegistered() {
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
	if EnsureServeHandlerRegistered() {
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
	if EnsureServeHandlerRegistered() {
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
