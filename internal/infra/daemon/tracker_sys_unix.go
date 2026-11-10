//go:build !windows && !js

package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"os"
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
	if pid <= 0 {
		return false
	}
	// On Linux, inspect /proc/<pid>/stat to distinguish zombies from running processes.
	// A zombie process has already terminated, but kill(pid, 0) still succeeds until reaped.
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if idx := bytes.LastIndexByte(data, ')'); idx != -1 && idx+2 < len(data) {
			state := data[idx+2]
			if state == 'Z' || state == 'X' {
				return false
			}
		}
	} else if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
		if _, statErr := os.Stat("/proc"); statErr == nil {
			return false
		}
	}
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
