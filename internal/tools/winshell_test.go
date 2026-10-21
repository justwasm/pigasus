// Tests for SelectWindowsShell, the Windows PowerShell 5.1 → PowerShell 7+
// resolution helper added for AGY 1.3.3 changelog line 47.
//
// The suite runs on every platform the clone builds (darwin, linux,
// windows) because the lookup is gated by an explicit goos parameter
// (selectWindowsShellOnOS). Only TestSelectWindowsShell_NonWindows returns
// early when the host is already Windows, since it exercises the public
// runtime-gated helper directly.
package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeShell creates an empty file with the given basename under dir
// and marks it executable. exec.LookPath on Unix checks m&0111 != 0, so
// the +x bit is required even though we never actually run the file.
func fakeShell(t *testing.T, dir, basename string) string {
	t.Helper()
	path := filepath.Join(dir, basename)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(%s): %v", path, err)
	}
	return path
}

// setPathOnly sets PATH to the given directories joined by the OS
// path list separator. t.Setenv restores the previous value when the
// test ends, so callers can run sequentially without leaking state.
func setPathOnly(t *testing.T, dirs ...string) {
	t.Helper()
	t.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
}

// TestSelectWindowsShell_NonWindows_ReturnsEmpty verifies the public
// helper returns "" on the host machine when host != Windows.
func TestSelectWindowsShell_NonWindows_ReturnsEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("non-windows host required")
	}
	if got := SelectWindowsShell(); got != "" {
		t.Errorf("SelectWindowsShell() = %q, want empty on %s", got, runtime.GOOS)
	}
}

// TestSelectWindowsShellOnOS_NonWindows_ReturnsEmpty covers every
// non-windows goos the clone might be built for, confirming the helper
// short-circuits regardless of the operating-system name.
func TestSelectWindowsShellOnOS_NonWindows_ReturnsEmpty(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "freebsd", "openbsd", "plan9", "js"} {
		if got := selectWindowsShellOnOS(goos); got != "" {
			t.Errorf("selectWindowsShellOnOS(%q) = %q, want empty", goos, got)
		}
	}
}

// TestSelectWindowsShellOnOS_BothPresent_PrefersPowershell places both
// powershell.exe and pwsh.exe on PATH and asserts powershell.exe is
// returned, since Windows PowerShell 5.1 ships with the OS and is the
// preferred default for backwards compatibility with v1.3.2.
func TestSelectWindowsShellOnOS_BothPresent_PrefersPowershell(t *testing.T) {
	dir := t.TempDir()
	fakeShell(t, dir, windowsShellPowershell)
	fakeShell(t, dir, windowsShellPwsh)
	setPathOnly(t, dir)

	got := selectWindowsShellOnOS("windows")
	if got == "" {
		t.Fatal("got empty, want powershell.exe")
	}
	if !strings.HasSuffix(got, windowsShellPowershell) {
		t.Errorf("got %q, want suffix %q", got, windowsShellPowershell)
	}
}

// TestSelectWindowsShellOnOS_OnlyPwsh_FallsBackToPwsh verifies the
// AGY 1.3.3 line 47 fix: when powershell.exe is missing but pwsh.exe
// is on PATH, the helper returns pwsh.exe instead of empty.
func TestSelectWindowsShellOnOS_OnlyPwsh_FallsBackToPwsh(t *testing.T) {
	dir := t.TempDir()
	fakeShell(t, dir, windowsShellPwsh)
	setPathOnly(t, dir)

	got := selectWindowsShellOnOS("windows")
	if got == "" {
		t.Fatal("got empty, want pwsh.exe fallback")
	}
	if !strings.HasSuffix(got, windowsShellPwsh) {
		t.Errorf("got %q, want suffix %q", got, windowsShellPwsh)
	}
	// And powershell.exe must NOT have been picked — the fallback is
	// the entire point of the test, so verify the suffix explicitly.
	if strings.HasSuffix(got, windowsShellPowershell) {
		t.Errorf("got %q, want pwsh.exe not powershell.exe", got)
	}
}

// TestSelectWindowsShellOnOS_NoShell_ReturnsEmpty confirms an empty
// PATH (or a PATH with neither powershell.exe nor pwsh.exe) yields "".
// Callers must treat that as a "no powershell found" error so the model
// can adapt its next tool call.
func TestSelectWindowsShellOnOS_NoShell_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir() // empty directory, no shells in it
	setPathOnly(t, dir)

	if got := selectWindowsShellOnOS("windows"); got != "" {
		t.Errorf("got %q, want empty when neither shell is on PATH", got)
	}
}

// TestSelectWindowsShellOnOS_PATHOrderHonored exercises PATH
// manipulation explicitly. With only pwsh.exe in dir1 and only
// powershell.exe in dir2, the helper must:
//   - return pwsh.exe       when PATH=dir1,
//   - return powershell.exe when PATH=dir2,
//   - return powershell.exe when PATH=dir1:dir2 (powershell wins).
func TestSelectWindowsShellOnOS_PATHOrderHonored(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	fakeShell(t, dir1, windowsShellPwsh)
	fakeShell(t, dir2, windowsShellPowershell)

	// PATH=dir1 → only pwsh reachable → must pick pwsh.
	setPathOnly(t, dir1)
	if got := selectWindowsShellOnOS("windows"); !strings.HasSuffix(got, windowsShellPwsh) {
		t.Errorf("PATH=dir1: got %q, want suffix %q", got, windowsShellPwsh)
	}

	// PATH=dir2 → only powershell reachable → must pick powershell.
	setPathOnly(t, dir2)
	if got := selectWindowsShellOnOS("windows"); !strings.HasSuffix(got, windowsShellPowershell) {
		t.Errorf("PATH=dir2: got %q, want suffix %q", got, windowsShellPowershell)
	}

	// PATH=dir1:dir2 → both reachable → powershell wins (first match
	// for the preferred lookup order). Order is intentional: this is
	// the changelog-documented behaviour for a host with both shells
	// installed.
	setPathOnly(t, dir1, dir2)
	if got := selectWindowsShellOnOS("windows"); !strings.HasSuffix(got, windowsShellPowershell) {
		t.Errorf("PATH=dir1:dir2: got %q, want suffix %q", got, windowsShellPowershell)
	}

	// PATH=dir2:dir1 → both reachable, but pwsh listed first. Lookup
	// still probes powershell first by name (not by PATH order for
	// the two candidate names) so powershell must still win. This
	// documents that the preference is a property of the candidate
	// list, not PATH ordering.
	setPathOnly(t, dir2, dir1)
	if got := selectWindowsShellOnOS("windows"); !strings.HasSuffix(got, windowsShellPowershell) {
		t.Errorf("PATH=dir2:dir1: got %q, want suffix %q", got, windowsShellPowershell)
	}
}
