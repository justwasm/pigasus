// Package procgroup is a backward-compatible facade over internal/features/tools/procgroup.
package procgroup

import (
	"os/exec"

	featureproc "github.com/justwasm/pigasus/internal/features/tools/procgroup"
)

func Set(cmd *exec.Cmd) {
	featureproc.Set(cmd)
}

func Kill(cmd *exec.Cmd) error {
	return featureproc.Kill(cmd)
}
