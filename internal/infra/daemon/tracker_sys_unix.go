//go:build !windows && !js

package daemon

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func ensureProcessGroup(cmd *exec.Cmd, pid int) int {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if !cmd.SysProcAttr.Setpgid {
		_ = syscall.Setpgid(pid, pid)
		cmd.SysProcAttr.Setpgid = true
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return pid
	}
	return pgid
}

func killProcessGroupGracefully(pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(50 * time.Millisecond)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

func isProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err != nil && errors.Is(err, syscall.ESRCH) {
		return false
	}
	return true
}

func signalTerm(target int) error {
	err := syscall.Kill(target, syscall.SIGTERM)
	if err != nil && errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func signalKill(target int) error {
	err := syscall.Kill(target, syscall.SIGKILL)
	if err != nil && errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
