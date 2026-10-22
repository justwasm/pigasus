package daemon

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// IsRunning reports whether pid is still alive. Used by tests to
// assert "the daemon is gone".
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	var status syscall.WaitStatus
	rpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	switch {
	case errors.Is(err, syscall.ECHILD):
		return IsRunningViaKill(pid)
	case errors.Is(err, syscall.ESRCH):
		return false
	case err != nil:
		return IsRunningViaKill(pid)
	case rpid == pid:
		return false
	case rpid == 0:
		return IsRunningViaKill(pid)
	}
	return IsRunningViaKill(pid)
}

// IsRunningViaKill is the slow path: send signal 0 and inspect the error.
func IsRunningViaKill(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	return errors.Is(err, syscall.EPERM)
}

// CmdFromArgs is a helper used by subagent and the test helpers:
// build an *exec.Cmd with Setpgid set, returning the cmd the caller
// should then hand to Track.
func CmdFromArgs(argv0 string, argv ...string) *exec.Cmd {
	cmd := exec.Command(argv0, argv...)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return cmd
}

var _ = os.Exit
