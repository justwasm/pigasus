//go:build unix || windows

package termguard

import (
	"os"

	"golang.org/x/term"
)

type termState struct {
	stdin  *term.State
	stdout *term.State
}

func captureState() *termState {
	var s termState
	if term.IsTerminal(int(os.Stdin.Fd())) {
		if st, err := term.GetState(int(os.Stdin.Fd())); err == nil {
			s.stdin = st
		}
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if st, err := term.GetState(int(os.Stdout.Fd())); err == nil {
			s.stdout = st
		}
	}
	return &s
}

func restoreState(s *termState) {
	if s == nil {
		return
	}
	if s.stdin != nil {
		_ = term.Restore(int(os.Stdin.Fd()), s.stdin)
	}
	if s.stdout != nil {
		_ = term.Restore(int(os.Stdout.Fd()), s.stdout)
	}
}
