//go:build windows

// Windows has no /dev/ptmx equivalent that the reference's pty package can
// drive — creack/pty itself ships only an ErrUnsupported stub there — so the
// clone runs the command with the caller's stdio. The reference's own
// GetDefaultShellName type switch has a Windows branch (powershell / pwsh),
// but its pty branch is the darwin/linux one; nothing on Windows was probed,
// so no pty is claimed here.
package tools

import "os/exec"

// RunWithPTYStdin runs cmd without a pseudo-terminal. See the unix build for
// the pty path and docs/cycles/cycle-277 for what that path reproduces.
func RunWithPTYStdin(cmd *exec.Cmd) error {
	return cmd.Run()
}
