// Tests for the non-darwin path of `remote-control start` — the
// foreground daemon.
//
// Where there is no launchd there is no *persistent* daemon: the
// launchd-backed registration is macOS-only, and the clone ships no
// systemd unit. The daemon itself is not macOS-only, so on every
// other platform `remote-control start` runs it in the foreground
// instead of failing with "launchd-backed mode requires macOS".
//
// The branch is exercised here on a darwin test rig through the
// rcIsDarwin seam — the branch is the whole point of the change, and
// CI does not run a Linux leg.
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// withNonDarwinPlatform makes the rcIsDarwin seam report a
// non-darwin platform for the duration of the test.
func withNonDarwinPlatform(t *testing.T) {
	t.Helper()
	orig := rcIsDarwin
	rcIsDarwin = false
	t.Cleanup(func() { rcIsDarwin = orig })
}

// TestRemoteControlStart_ForegroundWithoutLaunchd pins that
// `remote-control start` on a platform without launchd hands over to
// the foreground daemon instead of refusing.
func TestRemoteControlStart_ForegroundWithoutLaunchd(t *testing.T) {
	withNonDarwinPlatform(t)

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

	if err := runRCStart(j, []string{"--name", "test-machine", "--desktop"}); err != nil {
		t.Fatalf("runRCStart on a non-darwin platform failed: %v", err)
	}

	// The daemon ran in the foreground: the serve bridge was called.
	if captured.Name != "test-machine" {
		t.Errorf("the foreground handler got name %q, want %q", captured.Name, "test-machine")
	}
	if !captured.Desktop {
		t.Errorf("the foreground handler did not get --desktop")
	}
	// The label names the subcommand the user typed, so the daemon's
	// own progress lines read "remote-control start: …".
	if captured.Label != "remote-control start" {
		t.Errorf("the foreground handler got label %q, want %q", captured.Label, "remote-control start")
	}
	// The note on stderr says what happened and why.
	if !strings.Contains(errOut.String(), "no launchd") {
		t.Errorf("stderr does not explain the foreground mode: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "foreground") {
		t.Errorf("stderr does not say the daemon runs in the foreground: %q", errOut.String())
	}
	// The sentinel on stdout is the one `remote-control stop` prints
	// too, so a script can share it between the two.
	if !strings.Contains(out.String(), "Remote-control daemon stopped.") {
		t.Errorf("stdout missing the stop sentinel: %q", out.String())
	}
}

// TestRemoteControlStart_ForegroundWithoutTheBridge pins the failure
// mode: a binary built without the serve bridge (the parser surface
// alone) must say so instead of pretending to have started a daemon.
func TestRemoteControlStart_ForegroundWithoutTheBridge(t *testing.T) {
	withNonDarwinPlatform(t)

	orig := serveHandler
	serveHandler = nil
	t.Cleanup(func() { serveHandler = orig })

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{
		Name:   "pigasus",
		Out:    out,
		ErrOut: errOut,
	}

	err := runRCStart(j, []string{"--name", "test-machine"})
	if err == nil {
		t.Fatal("runRCStart without the serve bridge returned nil, want an error")
	}
	if !strings.Contains(err.Error(), "no handler registered") {
		t.Errorf("error = %q, want it to mention the missing handler", err.Error())
	}
}

// TestRemoteControlStop_NonDarwinNote pins the note `remote-control
// stop` prints on a platform without launchd: there is no registered
// daemon to stop — the daemon runs in the foreground, in the terminal
// that started it.
//
// The sentence on stdout stays the reference's own, so a script that
// greps for it keeps working.
func TestRemoteControlStop_NonDarwinNote(t *testing.T) {
	withNonDarwinPlatform(t)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{
		Name:   "pigasus",
		Out:    out,
		ErrOut: errOut,
	}

	if err := runRCStop(j, []string{"--name", "test-machine"}); err != nil {
		t.Fatalf("runRCStop on a non-darwin platform failed: %v", err)
	}
	if !strings.Contains(out.String(), "Remote-control daemon stopped.") {
		t.Errorf("stdout missing the reference's sentinel: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "foreground") {
		t.Errorf("stderr does not say where the daemon is: %q", errOut.String())
	}
}
