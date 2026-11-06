//go:build !windows && !js

package agentserver

import (
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
)

func startPTY(shell string, dir string, cols, rows int64) (*os.File, *exec.Cmd, error) {
	if shell == "" {
		shell = defaultShell()
	}
	cmd := exec.Command(shell, "-l")
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ws := &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, nil, err
	}
	return ptmx, cmd, nil
}

func defaultShell() string {
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		return sh
	}
	for _, cand := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return "sh"
}
