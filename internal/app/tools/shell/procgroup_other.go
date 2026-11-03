//go:build windows || js

package shell

import (
	"os/exec"

	"github.com/justwasm/pigasus/procgroup"
)

func setProcessGroup(cmd *exec.Cmd) {
	procgroup.Set(cmd)
}

func killProcessGroup(cmd *exec.Cmd) error {
	return procgroup.Kill(cmd)
}
