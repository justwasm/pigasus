package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/endpoints"
)

// TestUpdaterLockWarningIsTheReferences pins the reference's own
// wording for the advisory lock.
//
// 【文档】 official-docs/cli/troubleshooting.md:158 —
//
//	Warning: another background updater process is already active (update.lock)
//
// The bytes are the whole point: the line is what the user reads, and
// the "Warning: " belongs to it — the updater writes its diagnostics to
// stderr and prefixes them itself.
func TestUpdaterLockWarningIsTheReferences(t *testing.T) {
	const want = "Warning: another background updater process is already active (update.lock)"
	if updaterLockWarning != want {
		t.Errorf("warning = %q, want %q", updaterLockWarning, want)
	}
}

// TestUpdaterLockPathLivesInTheUpdatersOwnDirectory pins the lock's
// place.
//
// 【文档】 the reference keeps the lock in the updater's own directory
// inside the CLI's home (troubleshooting.md:163).
func TestUpdaterLockPathLivesInTheUpdatersOwnDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	want := filepath.Join(home, "updater", "update.lock")
	if got := updaterLockPath(); got != want {
		t.Errorf("lock path = %q, want %q", got, want)
	}
}

// TestAcquireUpdaterLockIsExclusive pins the lock's own behaviour: the
// first updater takes it, the second finds it there and steps aside,
// and the first one gives it back when it is done.
func TestAcquireUpdaterLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updater", "update.lock")

	release, ok := acquireUpdaterLock(path)
	if !ok {
		t.Fatal("the first acquirer did not get the lock")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the marker is not there: %v", err)
	}

	release2, ok2 := acquireUpdaterLock(path)
	if ok2 {
		release2()
		t.Fatal("the second acquirer got the lock as well")
	}

	release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the marker survived the release: %v", err)
	}

	// And the lock is free again.
	release3, ok3 := acquireUpdaterLock(path)
	if !ok3 {
		t.Fatal("the lock was not free after the release")
	}
	release3()
}

// TestAgyUpdateSkipsWhenAnotherUpdaterHoldsTheLock pins the whole
// path: an updater that finds the marker says the reference's warning
// and steps aside without updating.
func TestAgyUpdateSkipsWhenAnotherUpdaterHoldsTheLock(t *testing.T) {
	home := t.TempDir()
	lock := filepath.Join(home, "updater", "update.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Env = append(os.Environ(),
		endpoints.ConfigPathEnvVar+"="+filepath.Join(home, "config.json"),
		"ANTIGRAVITY_HOME="+home,
		"AGY_CURRENT_VERSION=1.4.10",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Fatalf("run failed: %v, stderr: %s", err, stderr.String())
	}

	const want = "Warning: another background updater process is already active (update.lock)\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing: the updater must step aside", stdout.String())
	}
	if strings.Contains(stdout.String(), "Checking for updates") {
		t.Errorf("the updater ran anyway: %q", stdout.String())
	}
}
