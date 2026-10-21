//go:build windows

// Windows stub for plugin_cleanup_testhelpers_unix.go. The signal-
// based tests are skipped on Windows anyway, so these helpers exist
// only to satisfy the call sites in plugin_cleanup_test.go.

package cli

import "os"

// unblockSignalZero is a Windows no-op; the matching tests skip with
// `runtime.GOOS == "windows"` before this is ever called.
func unblockSignalZero() os.Signal { return os.Kill }

// unblockSignalTerm mirrors the Unix helper for compilation purposes.
func unblockSignalTerm() os.Signal { return os.Kill }

// startSleepChild is unimplemented on Windows — the test that calls
// it also skips on Windows, but the symbol still has to exist for the
// compiler.
func startSleepChild(_ int) (int, error) { return 0, nil }

// itoa is a Windows-friendly shim for the sleep helper.
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
