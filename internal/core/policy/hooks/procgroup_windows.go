//go:build windows

package hooks

import (
	"os/exec"
)

// setProcessGroup sets the process cancellation handler on Windows.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	}
}
