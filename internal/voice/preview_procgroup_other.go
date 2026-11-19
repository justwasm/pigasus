//go:build windows || js || wasm

package voice

import "os/exec"

// attachPreviewProcessGroup is a no-op on non-Unix platforms (Windows / JS / WASM).
func attachPreviewProcessGroup(cmd *exec.Cmd) {}
