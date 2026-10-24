//go:build !windows

package procgroup

import (
	"os/exec"
	"syscall"
)

// Set puts the command in its own process group on Unix so the entire
// process tree can be signalled together.
func Set(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Kill terminates the process group associated with cmd.
// If the process group kill fails, it falls back to killing the primary process.
func Kill(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
			return nil
		}
	}
	return cmd.Process.Kill()
}
