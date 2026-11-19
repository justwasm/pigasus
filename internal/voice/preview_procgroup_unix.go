//go:build !windows && !js && !wasm

package voice

import (
	"os/exec"
	"syscall"
)

// attachPreviewProcessGroup puts the preview player in its own process group so
// a cancelled preview (context deadline, voice switch, hang-up) cannot leak the
// player as an orphan holding the sound card. Mirrors the isolation background
// command execution uses.
func attachPreviewProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Kill the whole group: the leader is the player itself.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
