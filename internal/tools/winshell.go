// Windows shell resolution. AGY 1.3.3 changelog line 47:
//
//	"Fixed Windows users without `powershell` (legacy 5.1) failing
//	 command execution — the CLI now falls back to `pwsh` (PowerShell 7+)
//	 when `powershell` is not available on PATH."
//
// On non-Windows platforms SelectWindowsShell is a no-op: the clone
// shells out to /bin/sh -c on macOS/Linux (cmd/wagyu dispatchTool) and
// the run_shell_command tool runs through mvdan.cc/sh/v3, which is
// platform-independent. Only the Windows branch of dispatchTool reaches
// SelectWindowsShell, and only when runtime.GOOS == "windows".
//
// We probe powershell.exe first because Windows PowerShell 5.1 ships
// with the OS; users who have not installed PowerShell 7 still get the
// legacy shell, matching the v1.3.2 behaviour the clone has to remain
// backwards-compatible with. When 5.1 is genuinely absent (Windows on
// ARM in S-mode, Server Core, stripped-down CI images) pwsh.exe is the
// fallback, so the agent keeps running.
//
// The exported SelectWindowsShell probes PATH via exec.LookPath, which
// honours PATHEXT on Windows and respects the user's PATH order — the
// same resolution a fresh cmd.exe uses.
//
// selectWindowsShellOnOS is the OS-aware internal helper. Splitting it
// out keeps the platform gate testable from non-Windows CI: every test
// in winshell_test.go passes goos="windows" explicitly so the suite
// runs everywhere this clone builds (darwin, linux, windows).
package tools

import (
	"os/exec"
	"runtime"
)

// Windows shell executables, in order of preference.
const (
	windowsShellPowershell = "powershell.exe"
	windowsShellPwsh       = "pwsh.exe"
)

// SelectWindowsShell returns the path to the PowerShell executable that
// should be used to invoke shell commands on this Windows host.
//
// Order of preference:
//   - powershell.exe (Windows PowerShell 5.1, bundled with Windows).
//   - pwsh.exe       (PowerShell 7+, cross-platform install).
//
// Returns "" if no PowerShell is on PATH. Callers MUST treat the
// empty-string case as an error so the model can see "no powershell
// found" verbatim and adapt its next tool call.
//
// On non-Windows platforms SelectWindowsShell returns "" (a no-op);
// the shell tool runs through /bin/sh -c (cmd/wagyu dispatchTool) or
// the pure-Go mvdan.cc/sh/v3 interpreter (RunShellCommand) instead
// and never reaches this code path.
func SelectWindowsShell() string {
	return selectWindowsShellOnOS(runtime.GOOS)
}

// selectWindowsShellOnOS is the OS-aware internal helper. Splitting it
// out from SelectWindowsShell keeps the platform gate testable from
// non-Windows CI: tests pass goos="windows" explicitly and exercise
// every PATH layout the Windows shell tool is expected to handle.
func selectWindowsShellOnOS(goos string) string {
	if goos != "windows" {
		return ""
	}
	if path, err := exec.LookPath(windowsShellPowershell); err == nil {
		return path
	}
	if path, err := exec.LookPath(windowsShellPwsh); err == nil {
		return path
	}
	return ""
}
