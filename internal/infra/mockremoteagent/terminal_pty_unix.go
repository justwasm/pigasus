//go:build !windows && !js

package mockremoteagent

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// openPTY starts cmd on a pseudoterminal and returns its master.
func openPTY(cmd *exec.Cmd) (*os.File, error) {
	return pty.Start(cmd)
}
