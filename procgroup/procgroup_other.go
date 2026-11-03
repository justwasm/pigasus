//go:build windows || js

package procgroup

import "os/exec"

// Set is a no-op on Windows where Setpgid does not exist.
func Set(cmd *exec.Cmd) {}

// Kill terminates the direct child process on Windows.
func Kill(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
