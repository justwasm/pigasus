//go:build !windows

// session_unix.go: the pty behind a terminal.
//
// 【一手】 2026-10-27, the reference's own process table: the
// language_server spawns `/opt/homebrew/bin/bash -l` — the user's
// `$SHELL`, as a *login* shell, in the terminal's directory. The
// title it reports is the shell's name ("bash").
//
// The pty itself is the same shape internal/tools builds for
// run_command (creack/pty): the child gets a new session with the
// pty slave as its controlling terminal, and the parent keeps only
// the master.
package rcterminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/creack/pty"
)

// The pty geometry the reference hands a fresh terminal. The Web UI
// resizes the terminal as soon as it is laid out, so this is only
// the starting point.
const (
	defaultRows = 24
	defaultCols = 80
)

// spawn starts a shell on a fresh pty.
func spawn(dir string) (*Session, error) {
	shell := shellPath()
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, fmt.Errorf("rcterminal: open pty: %w", err)
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: defaultRows, Cols: defaultCols}); err != nil {
		_ = tty.Close()
		_ = ptmx.Close()
		return nil, fmt.Errorf("rcterminal: set pty size: %w", err)
	}
	cmd := exec.Command(shell, "-l")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	// A new session with the pty slave as the controlling terminal:
	// the same disposition creack/pty's own Start uses.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = tty.Close()
		_ = ptmx.Close()
		return nil, fmt.Errorf("rcterminal: start shell: %w", err)
	}
	// The parent must drop the slave: holding it open wedges the
	// child in exit() (see internal/tools/pty_unix.go).
	_ = tty.Close()

	session := newSession(newTerminalID(), cmd.Process.Pid, filepath.Base(shell))
	session.cmd = cmd.Process
	session.ptmx = ptmx
	session.waitExit = func() int {
		if err := cmd.Wait(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return exitErr.ExitCode()
			}
			return -1
		}
		return 0
	}
	return session, nil
}

// shellPath is the shell the terminal runs. The reference uses the
// user's own shell; /bin/bash is the fallback.
func shellPath() string {
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		return sh
	}
	return "/bin/bash"
}

// pump reads the pty until the shell is gone.
func (s *Session) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.handleOutput(chunk)
		}
		if err != nil {
			break
		}
	}
	code := 0
	if s.waitExit != nil {
		code = s.waitExit()
	}
	s.finish(code)
}

// resize changes the pty's window size.
func (s *Session) resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited || s.closed {
		return ErrClosed
	}
	return pty.Setsize(s.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// close ends the terminal: the shell is signalled, the pty is
// closed, the streams are closed.
func (s *Session) close(force bool) {
	s.mu.Lock()
	s.closed = true
	proc := s.cmd
	s.mu.Unlock()

	if proc != nil {
		if force {
			_ = proc.Kill()
		} else {
			_ = proc.Signal(syscall.SIGHUP)
		}
	}
	// Closing the master hangs up the shell's session; the reader
	// goroutine then sees EOF and finishes.
	if s.ptmx != nil {
		_ = s.ptmx.Close()
	}
	s.closeSubscribers()
	close(s.done)
}
