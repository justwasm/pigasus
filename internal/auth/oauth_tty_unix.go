//go:build !windows

// OAuth code read via /dev/tty on POSIX — Open the controlling terminal
// for the read so the print-mode OAuth paste does not collide with a
// piped prompt on os.Stdin. Cycle 261 (AGY 1.1.2).
//
// /dev/tty is the canonical POSIX path to the controlling terminal of
// the current process; it is openable on every Linux / macOS / BSD
// platform we ship to. If the process has no controlling terminal
// (cron, CI runner with detached tty), os.Open returns an error and
// ReadAuthorizationCode surfaces the actionable AGY_OAUTH_TOKEN hint
// instead of blocking.
package auth

import (
	"io"
	"os"
)

// init wires the POSIX defaults into the package-level vars
// declared by oauth_tty.go. The (re)assignment is unconditional —
// tests that override these via t.Cleanup restore the pre-test
// values, not the values wired here, so platform wiring survives
// across test runs in the same process.
func init() {
	ControllingTerminalDevice = "/dev/tty"
	OpenControllingTerminalFn = func() (io.ReadCloser, error) {
		return os.Open(ControllingTerminalDevice)
	}
}
