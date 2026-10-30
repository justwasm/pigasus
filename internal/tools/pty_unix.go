//go:build !windows && !js

// Pseudo-terminal plumbing for run_command.
//
// The reference gives a command a pty on **stdin only** — stdout/stderr stay
// pipes. Live 1.4.2 probe (一手, docs/cycles/cycle-277):
//
//	TTY0=/dev/ttys005        # stdin is a tty
//	TTY1=not a tty           # stdout is a pipe
//	PARENT=/Users/gear/.local/bin/agy
//	speed 9600 baud; rows 24; columns 80;
//	icanon echo isig icrnl opost onlcr ixany imaxbel -iutf8
//	$-=569X                  # zsh -c, no -i, no -l
//
// The execution chain that builds it is
// cortex/command/command.(*goRunner).Run -> google3/devtools/ai/sandbox/exebox
// -> google3/third_party/golang/pty (pty.open / pty.ptsname / pty.ioctl) —
// the same openpty-via-ioctl shape creack/pty implements, which is why the
// clone uses that module rather than hand-rolling TIOCPTYGRANT.
package tools

import (
	"io"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// referencePTYRows / referencePTYCols are the geometry the reference hands the
// shell; the live termios probe above reports exactly these values.
const (
	referencePTYRows = 24
	referencePTYCols = 80
)

// vEOF is the default end-of-file character (^D) in canonical mode. See
// RunWithPTYStdin for why the clone writes it.
const vEOF = 0x04

// RunWithPTYStdin starts cmd with stdin on a fresh 80x24 pty, waits for it and
// returns the wait error. cmd.Stdout / cmd.Stderr are left alone, so the caller
// keeps its pipes — which is the reference's split (tty stdin, pipe stdout).
//
// Why a pty at all: programs change behaviour when stdin is a terminal —
// `git` colourises, `[ -t 0 ]` flips, some tools refuse to run on a pipe.
// With it, `tty` prints /dev/ttysNNN, `[ -t 0 ]` is true, `stty -a` reports
// 24x80, and `$-` comes out as 569X, all matching the reference probe above.
//
// Deliberate deviation, documented in docs/cycles/cycle-277: the reference's
// pty belongs to a *persistent terminal* whose input end stays open, so a
// command that reads stdin (`read x`, bare `cat`) blocks, the tool's
// WaitMsBeforeAsync elapses and the command is handed to the background as a
// task. The clone's run_command is synchronous and ignores
// WaitMsBeforeAsync, so blocking there would wedge the whole turn with no
// escape. Instead it writes a single VEOF (^D) to the pty master right after
// the child starts; the first canonical read then sees EOF and returns, which
// is the same result the reference eventually produces for those commands
// (`read x; echo DONE:${x:-eof}` -> `DONE:eof`, exit 0, reproduced on both).
// Commands that never touch stdin are unaffected.
//
// Three measurements shape the rest of this function (docs/cycles/cycle-277):
//
//   - The parent must drop its copy of the slave fd right after Start.
//     Holding it open leaves the child wedged in `exit()` — `ps` shows
//     `STAT ?Es` (E = "trying to exit") and Wait4 never returns, for even a
//     trivial `zsh -c 'echo hello'`. creack/pty's own Start closes it there
//     too.
//   - The master must be drained. The pty echoes our ^D back up to the
//     master, and with nobody reading it Darwin holds the child's exit for a
//     fixed ~600 ms waiting for that output to drain (`sleep 1` took 1.62 s
//     with echo on and no reader, 1.02 s once drained). Draining rather than
//     disabling ECHO keeps the termios identical to the reference's probe.
//   - The master must not be closed while the child lives: that delivers
//     SIGHUP to the session's controlling process group and killed the probe
//     outright (`err=signal: hangup`, empty output).
//
// Consequence of the drain: output a command writes to /dev/tty (rather than
// to fd 1/2) is discarded, since only the pipes reach the tool result. The
// reference's own captured result text contains stdout+stderr only, so this
// was not observed to differ.
func RunWithPTYStdin(cmd *exec.Cmd) error {
	wait, _, err := startWithPTYStdin(cmd, true)
	if err != nil {
		return err
	}
	return wait()
}

// startWithPTYStdin is RunWithPTYStdin split in two: it starts cmd with a
// pty on stdin and hands back the wait function, so a caller that has to
// wait with a deadline can start the command, give it the deadline, and
// still let it run on when the deadline passes.
//
// eof writes the VEOF the clone uses to end the input side (see the
// deviation note above). The reference does not: its pty belongs to a
// persistent terminal whose input end stays open, so a command that reads
// stdin blocks until the reference's WaitMsBeforeAsync elapses and the
// command is handed to the background as a task. A backgrounded command
// keeps its stdin open so a later send_command_input can still write to
// it — hence eof=false on that path.
//
// The returned stdin writer is the pty master: writing to it sends input
// to the child. It stays valid until the wait function returns.
func startWithPTYStdin(cmd *exec.Cmd, eof bool) (func() error, io.WriteCloser, error) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		// No pty (e.g. a resource-limited host): fall back to running with
		// the caller's stdio rather than failing the command.
		return func() error { return cmd.Run() }, nil, nil
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: referencePTYRows, Cols: referencePTYCols}); err != nil {
		tty.Close()
		ptmx.Close()
		return func() error { return cmd.Run() }, nil, nil
	}

	cmd.Stdin = tty
	// New session + controlling terminal, the same disposition creack/pty's
	// own Start uses. ctty 0 is the child's fd for Stdin, i.e. the pty slave.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}

	if err := cmd.Start(); err != nil {
		tty.Close()
		ptmx.Close()
		return nil, nil, err
	}
	tty.Close()
	go io.Copy(io.Discard, ptmx)
	if eof {
		// Deliver EOF through the line discipline. A failure here is not
		// fatal — the command may simply not read stdin, and by then the
		// slave is gone.
		_, _ = ptmx.Write([]byte{vEOF})
	}
	wait := func() error {
		err := cmd.Wait()
		ptmx.Close()
		return err
	}
	return wait, ptmx, nil
}
