//go:build !windows

// Process-group control for bash-mode commands.
//
// A shell command such as `sh -c 'echo first; sleep 30'` leaves a grandchild
// holding the stdout pipe after the shell itself is killed. Without killing
// the group, exec.Cmd.Wait blocks until that grandchild exits on its own, so
// Esc would not actually end the turn (measured on wagyu: the spinner stayed
// alive for the full 30 s). The reference stops the whole tree, which is why
// a preempted command's successor runs immediately.
package main

import (
	"os/exec"
	"syscall"
)

// bashSetProcessGroup puts the child in its own process group so the whole
// tree can be signalled with a single negative pid.
func bashSetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// bashKillProcessGroup SIGKILLs the child's process group.
func bashKillProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
