//go:build !windows

// Unix-only test helpers for plugin_cleanup_test.go. The signal-based
// tests in that file are skipped on Windows anyway (see the per-test
// `runtime.GOOS == "windows"` guard), so these helpers live behind a
// build tag to keep the Windows build green.

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

// unblockSignalZero returns a signal value that, when delivered, asks
// the kernel "is this pid alive?" without actually signalling it.
// signal 0 is the standard POSIX way to probe a process; Go does not
// expose it as a named constant.
func unblockSignalZero() os.Signal {
	return syscall.Signal(0)
}

// unblockSignalTerm returns SIGTERM — what the production kill path
// uses to ask MCP servers and sidecars to wind down cleanly.
func unblockSignalTerm() os.Signal {
	return syscall.SIGTERM
}

// startSleepChild spawns `sleep <secs>` and returns its PID. The
// 30-second window is long enough for a sub-second test to interact
// with the child, short enough that a forgotten child never lives
// past the test run.
func startSleepChild(seconds int) (int, error) {
	cmd := exec.Command("sleep", itoa(seconds))
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	// Detach so the parent can exit without taking the child with it.
	go func() { _ = cmd.Wait() }()
	return cmd.Process.Pid, nil
}

// itoa is a tiny strconv.Itoa shim so the file does not pull in the
// full strconv import for one call site. It is only used by the
// sleep helper which has no other dependencies.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
