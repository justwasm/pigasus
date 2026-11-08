//go:build windows || js

package mockremoteagent

import (
	"errors"
	"os"
	"os/exec"
)

// openPTY is unavailable on this platform.
func openPTY(*exec.Cmd) (*os.File, error) {
	return nil, errors.New("mockremoteagent: terminals are not supported on this platform")
}
