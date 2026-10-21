//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// setProcessGroup configures the command to start in its own process group on Unix.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	} else {
		cmd.SysProcAttr.Setpgid = true
	}
}

// killProcessGroup terminates the process group associated with cmd.
// If the process group kill fails, it falls back to killing the primary process.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		// Negative PID sends the signal to the entire process group.
		if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
			return nil
		}
	}
	return cmd.Process.Kill()
}
