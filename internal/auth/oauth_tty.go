// Package auth — oauth_tty.go: read an OAuth authorization code from the
// controlling terminal when stdin is consumed by a piped prompt.
//
// Cycle 261 (AGY 1.1.2)
//
// # Background
//
// When antigravity is launched with `agy -p "fix bug"` and stdin is piped
// (e.g. `echo 'fix bug' | agy -p`), the prompt rides on stdin and the user
// cannot paste the OAuth authorization code via the same channel — the
// piped prompt would consume the paste, and any reader of os.Stdin would
// block forever waiting for input that has already arrived.
//
// The fix mirrors the AGY 1.1.2 changelog line: read the OAuth code from
// the controlling-terminal device (/dev/tty on POSIX, CONIN$ on Windows)
// when stdin is not a terminal. If opening the controlling-terminal
// device fails (the truly headless case — e.g. a CI runner with no tty
// attached), ReadAuthorizationCode returns an actionable error that
// points the operator at the AGY_OAUTH_TOKEN env var instead of
// blocking on a read that will never resolve.
//
// The original stdin remains the prompt source; only the OAuth code
// read goes via /dev/tty. The prompt reader and the OAuth code reader
// are separate — readers of os.Stdin are unaffected.
//
// # Test seams
//
// StdinIsTTYFn, OpenControllingTerminalFn and ControllingTerminalDevice
// are package-level vars that callers override in tests. The defaults
// wired by oauth_tty_unix.go (POSIX /dev/tty) and oauth_tty_windows.go
// (CONIN$) are overridden by init() functions in those build-tagged
// files; the same fn is exported on every platform so callers and
// tests do not need to branch on GOOS.
package auth

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// controllingTerminalReadError is the actionable error returned when
// neither stdin nor the controlling-terminal device can be read from.
// The wording mirrors the AGY 1.1.2 changelog contract: the user is
// pointed at AGY_OAUTH_TOKEN (the env var that supplies a token
// directly so no paste is needed at all).
const controllingTerminalReadError = "OAuth authorization requires a controlling terminal; use a TTY or set AGY_OAUTH_TOKEN env var to provide a token directly."

// ReadAuthorizationCode reads a single OAuth authorization code line
// (terminated by '\n', trailing newline and surrounding whitespace
// stripped) from one of two sources:
//
//  1. r — the caller-supplied reader, used directly when StdinIsTTYFn()
//     reports that the process is attached to a terminal. In print
//     mode this is os.Stdin itself; the OAuth paste echoes back to
//     the user's own terminal exactly as before.
//
//  2. The controlling-terminal device opened by OpenControllingTerminalFn()
//     — used when stdin is not a terminal (piped prompt path). The
//     caller wires the platform default (oauth_tty_unix.go opens
//     /dev/tty; oauth_tty_windows.go opens CONIN$); tests substitute
//     a fake.
//
// When stdin is not a terminal and the controlling-terminal device
// cannot be opened — the truly headless case — ReadAuthorizationCode
// returns controllingTerminalReadError and the caller fails fast
// rather than blocking forever on the read.
//
// ReadAuthorizationCode never closes r. The controlling-terminal
// reader is closed before returning.
func ReadAuthorizationCode(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("auth.ReadAuthorizationCode: nil reader")
	}
	if StdinIsTTYFn != nil && StdinIsTTYFn() {
		return readOneLine(r)
	}
	if OpenControllingTerminalFn == nil {
		return "", errors.New(controllingTerminalReadError)
	}
	tty, err := OpenControllingTerminalFn()
	if err != nil {
		return "", fmt.Errorf("%s (open %s: %w)", controllingTerminalReadError, ControllingTerminalDevice, err)
	}
	defer tty.Close()
	return readOneLine(tty)
}

// readOneLine reads up to and including a '\n', trims trailing
// whitespace, and returns the result. io.EOF (closed pipe with no
// newline terminator) is treated as a successful read of the bytes
// received so far, matching Go's convention for short-pasted codes
// that the user terminated with Enter-or-D^D on the controlling
// terminal.
func readOneLine(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("auth.readOneLine: nil reader")
	}
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("auth.readOneLine: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// StdinIsTTYFn reports whether the process is attached to a terminal
// on stdin. Tests override it to simulate the TTY (true) and piped
// (false) cases without touching os.Stdin. The default implementation
// uses golang.org/x/term.IsTerminal against os.Stdin.Fd(); it is nil
// at package init briefly but the OAuth code path is only entered
// after main()'s flag parsing completes, so a non-nil value is
// always observed at runtime.
//
// Set via WithStdinIsTTY for type safety; the var is exported to keep
// the test seam symmetric with the other auth-package vars.
var StdinIsTTYFn = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// OpenControllingTerminalFn opens the controlling-terminal device so
// the OAuth code path can read from the user's terminal even when
// the process stdin is consumed by a piped prompt. Tests override it
// to inject a fake reader or to simulate a failed open.
//
// The default is set by init() in oauth_tty_unix.go (POSIX /dev/tty)
// or oauth_tty_windows.go (Windows CONIN$). It is consulted lazily
// — only when StdinIsTTYFn returns false — so a nil value is fine
// for callers that always run on a TTY (interactive login, etc.).
var OpenControllingTerminalFn func() (io.ReadCloser, error)

// ControllingTerminalDevice is the path (POSIX) or symbolic name
// (Windows) of the controlling-terminal device opened by
// OpenControllingTerminalFn. It is set by init() in oauth_tty_unix.go
// ("/dev/tty") and oauth_tty_windows.go ("CONIN$"). ReadAuthorizationCode
// mentions it in error messages so the operator can verify which
// path the binary tried to open.
//
// Default empty string before init() runs; production callers always
// observe the platform value.
var ControllingTerminalDevice string
