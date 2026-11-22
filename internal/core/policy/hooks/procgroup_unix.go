//go:build !windows

package hooks

import (
	"os/exec"
	"syscall"
)

// setProcessGroup ensures the hook command runs in its own process group
// and that context cancellation/timeout kills the entire process group (AGY 1.5.14).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
