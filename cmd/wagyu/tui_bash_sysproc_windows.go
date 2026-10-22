//go:build windows

// Windows has no Setpgid process groups, so cancellation falls back to
// killing the direct child. Same shape as the unix file so
// tui_bash_mode.go stays platform-free.
package main

import "os/exec"

func bashSetProcessGroup(cmd *exec.Cmd) {}

func bashKillProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
