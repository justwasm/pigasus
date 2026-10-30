//go:build windows || js

package daemon

import (
	"os"
	"os/exec"
)

func ensureProcessGroup(cmd *exec.Cmd, pid int) int {
	return pid
}

func killProcessGroupGracefully(pgid int) {
	if pgid < 0 {
		pgid = -pgid
	}
	p, err := os.FindProcess(pgid)
	if err == nil && p != nil {
		_ = p.Kill()
	}
}

func isProcessAlive(pid int) bool {
	return false
}

func signalTerm(target int) error {
	if target < 0 {
		target = -target
	}
	p, err := os.FindProcess(target)
	if err == nil && p != nil {
		_ = p.Kill()
	}
	return nil
}

func signalKill(target int) error {
	if target < 0 {
		target = -target
	}
	p, err := os.FindProcess(target)
	if err == nil && p != nil {
		_ = p.Kill()
	}
	return nil
}
