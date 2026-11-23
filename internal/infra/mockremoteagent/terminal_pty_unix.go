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

// resizePTY updates the pseudoterminal window size.
func resizePTY(f *os.File, cols, rows int) error {
	return pty.Setsize(f, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}
