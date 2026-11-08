package tuistore

// bash_runtime.go — the running shell command's cancellation.
//
// The store already owns the command's row, generation and queue. This
// file gives the same owner the cancel function, so the TUI host does not
// reach into the field to stop a command.

import "context"

// BashRunning reports whether a shell command is in flight.
func (s *Store) BashRunning() bool {
	return s != nil && s.BashCancel != nil
}

// SetBashCancel installs the running command's cancel function.
func (s *Store) SetBashCancel(cancel context.CancelFunc) {
	if s == nil {
		return
	}
	s.BashCancel = cancel
}

// CancelBash cancels the running shell command and clears the function.
// It reports whether a command was running.
func (s *Store) CancelBash() bool {
	if s == nil || s.BashCancel == nil {
		return false
	}
	s.BashCancel()
	s.BashCancel = nil
	return true
}
