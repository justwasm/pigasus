//go:build windows

package voice

import "os/exec"

// attachPreviewProcessGroup is a no-op on Windows: the player is a single
// process and the context deadline already terminates it.
func attachPreviewProcessGroup(cmd *exec.Cmd) {}
