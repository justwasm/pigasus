package workspace

import (
	"os"
	"testing"
)

// TestCurrentReturnsTheWorkingDirectory pins the answer: the process's
// own working directory.
//
// The package is the one owner of that fact, so the callers — the
// conversation store, the hosts — read it from here.
func TestCurrentReturnsTheWorkingDirectory(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if got := Current(); got != wd {
		t.Fatalf("Current() = %q, want %q", got, wd)
	}
}
