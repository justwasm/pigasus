//go:build !windows

package tools

import (
	"os/exec"

	"github.com/justwasm/pigasus/internal/procgroup"
)

// setProcessGroup configures the command to start in its own process group on Unix.
func setProcessGroup(cmd *exec.Cmd) {
	procgroup.Set(cmd)
}

// killProcessGroup terminates the process group associated with cmd.
func killProcessGroup(cmd *exec.Cmd) error {
	return procgroup.Kill(cmd)
}
