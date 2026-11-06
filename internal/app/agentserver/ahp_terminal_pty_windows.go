//go:build windows

package agentserver

import (
	"errors"
	"os"
	"os/exec"
)

func startPTY(shell string, dir string, cols, rows int64) (*os.File, *exec.Cmd, error) {
	return nil, nil, errors.New("terminal pty is not supported on windows")
}
