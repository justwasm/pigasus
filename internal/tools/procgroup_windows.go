//go:build windows

package tools

import (
	"os/exec"

	"github.com/justwasm/pigasus/internal/procgroup"
)

func setProcessGroup(cmd *exec.Cmd) {
	procgroup.Set(cmd)
}

func killProcessGroup(cmd *exec.Cmd) error {
	return procgroup.Kill(cmd)
}
