//go:build windows

// OAuth code read via CONIN$ on Windows — Open the controlling console's
// standard input handle so the print-mode OAuth paste does not collide
// with a piped prompt on os.Stdin. Cycle 261 (AGY 1.1.2).
//
// CONIN$ is the Windows console reserved device name for the
// console's standard input handle. os.OpenFile with os.O_RDONLY opens
// it for read; the handle is shared with the running session so the
// paste echoes through the same terminal the user is staring at.
// If the process has no console (e.g. spawned via CreateProcess with
// CREATE_NO_WINDOW), the open returns an error and ReadAuthorizationCode
// surfaces the actionable AGY_OAUTH_TOKEN hint instead of blocking.
package auth

import (
	"io"
	"os"
)

func init() {
	ControllingTerminalDevice = "CONIN$"
	OpenControllingTerminalFn = func() (io.ReadCloser, error) {
		return os.OpenFile(ControllingTerminalDevice, os.O_RDONLY, 0)
	}
}
