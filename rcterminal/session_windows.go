//go:build windows

// session_windows.go: the Windows stub.
//
// Windows has no /dev/ptmx the clone can drive — creack/pty itself
// ships only an ErrUnsupported stub there — so a terminal cannot be
// created. The reference's own shell handling has a Windows branch
// (powershell / pwsh), but its pty branch is the darwin/linux one;
// nothing on Windows was probed, so no pty is claimed here.
//
// The registry, the messages and the stream frames are the same on
// every platform: only the process behind a terminal is missing.
package rcterminal

import (
	"errors"
	"fmt"
)

// ErrUnsupported is the error a terminal creation answers on
// Windows.
var ErrUnsupported = errors.New("rcterminal: terminals need a pty, which this platform does not provide")

// spawn fails: there is no pty here.
func spawn(dir string) (*Session, error) {
	return nil, fmt.Errorf("%w (directory %q)", ErrUnsupported, dir)
}

// pump does nothing: there is no pty here.
func (s *Session) pump() {}

// resize fails: there is no pty here.
func (s *Session) resize(cols, rows int) error {
	return ErrUnsupported
}

// close ends the terminal.
func (s *Session) close(force bool) {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.closeSubscribers()
	close(s.done)
}
