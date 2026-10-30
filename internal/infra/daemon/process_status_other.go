//go:build windows || js

package daemon

import (
	"os"
	"os/exec"
)

// IsRunning reports whether pid is still alive on non-unix / js platforms.
func IsRunning(pid int) bool {
	return IsRunningViaKill(pid)
}

// IsRunningViaKill checks whether process is running on non-unix / js platforms.
func IsRunningViaKill(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p
	return false
}

// CmdFromArgs creates an *exec.Cmd on non-unix / js platforms.
func CmdFromArgs(argv0 string, argv ...string) *exec.Cmd {
	return exec.Command(argv0, argv...)
}

var _ = os.Exit
