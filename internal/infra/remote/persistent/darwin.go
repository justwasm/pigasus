//go:build darwin

// darwin.go: the macOS / launchd implementation of the
// persistentrc package's Start / Stop / Status surface.
//
// This file owns the orchestration layer — `Start`, `Stop`,
// `pidAlive`, and the `dryRun` test-mode flag.  The leaf helpers
// (plist generation / parsing, launchctl invocation / print
// parsing, path resolution) live in the per-concern sibling files:
//
//	darwin_plist.go      — buildPlist, writePlist, parsePlistProgramArgs*
//	darwin_paths.go      — plistPath, pidfilePath, readPidfile, startedAtFromPlist
//	darwin_launchctl.go  — launchctlUID, bootstrapLaunchd, bootoutLaunchd,
//	                      readLaunchdPrint, parsePIDFromLaunchctl
//	darwin_status.go     — Status + statusReport / statusFromLaunchctl /
//	                      statusFromDryRun + addrFromPlist
//
// Lifecycle:
//
//   - Start(ctx, spec)
//       1. resolveSpec(spec) — fill defaults, validate.
//       2. writePlist(spec) — write the LaunchAgent plist to
//          ~/Library/LaunchAgents/com.openagy.remote-control-<name>.plist
//          (or spec.PlistDir for tests).
//       3. bootstrapLaunchd(plist) — `launchctl bootstrap
//          gui/$UID <plist>` so launchd picks it up.  RunAtLoad
//          causes immediate spawn.
//       4. Sleep 200 ms (the brief's "verify the PID is alive"
//          step), then readLaunchdPrint(label) and assert the
//          reported pid is non-zero.
//       5. On Spec.SkipLaunchd: skip the launchctl calls;
//          write a fake .pid file alongside the plist so Status
//          can read it back without invoking launchctl.  This
//          is what the dry-run tests use.
//
//   - Stop(ctx, name)
//       1. bootoutLaunchd(label) — `launchctl bootout
//          gui/$UID/<label>` (the inverse of bootstrap).
//       2. Remove the plist.
//       3. If a foreground process is still alive (the dry-run
//          pidfile), kill it with SIGTERM, wait 2 s, then SIGKILL.
//
//   - Status(ctx, name) — see darwin_status.go for the body and
//      the statusFromLaunchctl / statusFromDryRun split.

package persistent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Start brings up the daemon.  See the package-level doc for the
// full lifecycle.
//
// On Spec.SkipLaunchd:
//   - write the plist into Spec.PlistDir
//   - write a fake pidfile with our own PID
//   - return immediately
//
// On macOS production:
//   - write the plist into ~/Library/LaunchAgents/
//   - bootstrap launchd
//   - sleep 200 ms, read launchctl print, assert non-zero PID
//   - report error if launchctl didn't pick up the plist
//
// Errors:
//   - ErrInvalidAddr / ErrInvalidName from validateSpec
//   - writePlist failures (mkdir / WriteFile)
//   - bootstrapLaunchd failures (launchctl exit-non-zero)
//   - readLaunchdPrint returning 0 (bootstrap succeeded but the
//     agent never came up within the 200 ms grace; treat as
//     "daemon did not start; check <log>")
func Start(ctx context.Context, spec Spec) error {
	resolved, err := ResolveSpec(spec)
	if err != nil {
		return err
	}
	if resolved.BinaryPath == "" {
		// ResolveSpec leaves BinaryPath empty; the caller
		// is expected to fill it via os.Executable before
		// calling Start.  Tests inject an explicit path.
		return errors.New("persistentrc: Spec.BinaryPath is required (use os.Executable() to fill it)")
	}
	if err := writePlist(resolved); err != nil {
		return err
	}
	if err := bootstrapLaunchd(resolved); err != nil {
		return err
	}
	if resolved.SkipLaunchd {
		// No launchd to verify against; the dry-run
		// pidfile + plist are the source of truth.
		return nil
	}
	// Verify the agent came up within the brief's 200 ms
	// grace.  In practice launchd takes ~50 ms to spawn the
	// child on a fresh bootstrap, but the brief pins 200 ms
	// so we'll do the same.
	select {
	case <-time.After(200 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}
	pid, err := readLaunchdPrint(resolved.Name)
	if err != nil || pid <= 0 {
		// Cleanup so we don't leave a half-registered plist
		// lying around; matches the reference's
		// "daemon did not start; check %s" path.
		_ = bootoutLaunchd(resolved.Name)
		_ = os.Remove(plistPath(resolved))
		if err != nil {
			return fmt.Errorf("daemon did not start; check %s (last error: %w)", resolved.LogFile, err)
		}
		return fmt.Errorf("daemon did not start; check %s", resolved.LogFile)
	}
	return nil
}

// Stop unregisters the daemon.  Idempotent — "no such service"
// exit codes from launchctl are treated as success.
//
// Order of operations:
//
//  1. launchctl bootout
//  2. remove plist (using the active plist-dir override so
//     tests clean up their temp dir properly)
//  3. if a dry-run pidfile exists, SIGTERM the recorded PID
//     (best-effort), wait 2 s, then SIGKILL — skipped when the
//     global dryRun flag is set, since the pidfile points at
//     the test process itself and signalling it would
//     terminate the test.
//  4. remove pidfile
func Stop(ctx context.Context, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := bootoutLaunchd(name); err != nil {
		// bootout is best-effort; we still want to clean
		// up the plist so the next Start sees a fresh tree.
		_ = err
	}
	if dir := activePlistDir(); dir != "" {
		p := filepath.Join(dir, Label(name)+".plist")
		_ = os.Remove(p)
		// Drop any side-car files the dry-run mode wrote
		// so the next Start sees a clean tree.
		_ = os.Remove(p + ".started_at")
	}
	// Dry-run cleanup.  When the global dryRun flag is set, the
	// pidfile points at a fake PID (or, for tests, the test
	// process itself); signalling that PID would terminate the
	// test, so we skip the signal and just remove the pidfile.
	// Production mode (dryRun == false) never writes a pidfile,
	// so this branch is a no-op outside tests.
	spec := Spec{Name: name}
	if pid, _ := readPidfile(spec); pid > 0 {
		if !dryRun {
			_ = syscall.Kill(pid, syscall.Signal(unixSignalTERM))
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if !pidAlive(pid) {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if pidAlive(pid) {
				_ = syscall.Kill(pid, syscall.Signal(unixSignalKILL))
			}
		}
		removePidfile(spec)
	}
	return nil
}

// pidAlive reports whether pid is currently a live process.
// signal 0 is the standard "is this process alive?" probe on
// POSIX.  Returns false on ESRCH.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	// EPERM means the process exists but we can't signal it
	// — count that as alive so we don't try to spawn a
	// second daemon over it.
	return errors.Is(err, syscall.EPERM)
}

// isDryRun is the global test-mode flag.  Production never sets
// this; tests set PERSISTENTRC_TEST_DRYRUN=1 before invoking
// Start / Status so the package skips launchctl entirely.
//
// Set via setDryRun() in tests; the var indirection lets us
// reset between tests without leaking state.
var dryRun = false

func isDryRun() bool { return dryRun }

// SetDryRun is the test-only entry point.  Production callers
// never invoke it.
func SetDryRun(v bool) { dryRun = v }
