//go:build !darwin

// other.go: the non-darwin (Linux / Windows) implementation of the
// persistentrc package's Start / Stop / Status surface.
//
// The cycle-11 brief calls for macOS launchd as the primary
// target — the machine AGENTS.md documents is darwin/arm64 — and
// Linux/Windows "get a polite 'not supported on this platform'
// message but the wiring is plumbed enough that adding systemd
// later is straightforward".  This file is the wiring:
//
//   - Start returns ErrUnsupported unless SkipLaunchd is set
//     (test-only).  The error message names the platform so the
//     CLI surfaces a clear "macOS-only" hint and points at the
//     foreground path (`remote-control run` runs the daemon in the
//     foreground here).
//   - Stop is a no-op (no plist to clean up).  Returns nil so
//     `remote-control stop` exits 0 even on Linux/Windows.
//   - Status returns a zero-value report with a hint field
//     pointing at the foreground subcommand.  Always returns
//     running=false; no platform-specific report.
//
// We deliberately do NOT add a setsid-detach fallback here even
// though Linux's `--session` flag could in principle use it —
// the brief's "not supported" message covers that path, and a
// setsid-detached process still leaves no auto-restart on crash
// (which is the whole point of the persistent daemon).  A future
// systemd cycle can add that.

package persistentrc

import (
	"context"
	"errors"
	"fmt"
	"runtime"
)

// Start on a non-darwin platform returns ErrUnsupported unless
// the caller has set Spec.SkipLaunchd (test mode).  Production
// callers should never set SkipLaunchd.
func Start(ctx context.Context, spec Spec) error {
	if spec.SkipLaunchd {
		// Test-only dry-run: pretend to start.  The
		// StatusReport / plist write path the tests rely
		// on is in the platform-specific files; here we
		// just succeed so the test rig gets a usable
		// Spec out.
		return nil
	}
	return fmt.Errorf("%w (running on %s/%s); run `remote-control run` to run the daemon in the foreground",
		ErrUnsupported, runtime.GOOS, runtime.GOARCH)
}

// Stop is a no-op on non-darwin.  Returns nil so the CLI exits
// 0 even when the agent was never registered.  Idempotent.
func Stop(ctx context.Context, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	return nil
}

// Status returns the canonical "not running" report on
// non-darwin.  Always returns running=false; the hint field
// points at the per-session variant the rest of the stack
// supports.
func Status(ctx context.Context, name string) (StatusReport, error) {
	if err := ValidateName(name); err != nil {
		return StatusReport{}, err
	}
	return StatusReport{
		Running: false,
		Name:    name,
		Hint:    fmt.Sprintf("the persistent remote-control daemon needs launchd, which only macOS has (running on %s/%s); `remote-control run` runs the daemon in the foreground here", runtime.GOOS, runtime.GOARCH),
	}, nil
}

// pidfilePath is a stub on non-darwin so the package compiles.
// Tests that exercise the dry-run path on Linux/Windows set
// Spec.SkipLaunchd; they don't reach into this file's helpers.
func pidfilePath(spec Spec) string {
	if spec.PlistPath != "" {
		return spec.PlistPath + ".pid"
	}
	return ""
}

// readPidfile is a stub; dry-run mode on non-darwin doesn't
// produce a pidfile.  Tests that need a fake PID inject it
// directly through Spec.
func readPidfile(spec Spec) (int, error) {
	return 0, nil
}

// removePidfile is a stub.
func removePidfile(spec Spec) {}

// isDryRun is the global test-mode flag.  Production never
// sets this; tests set PERSISTENTRC_TEST_DRYRUN=1 before
// invoking Start / Status so the package skips launchctl
// entirely.  On non-darwin we honour the same flag so the test
// rig is identical across platforms.
var dryRun = false

func isDryRun() bool { return dryRun }

// SetDryRun is the test-only entry point.
func SetDryRun(v bool) { dryRun = v }

// silenceUnused keeps the package compile-clean when this file
// is the only one compiled (no darwin build tag matched).  We
// reference errors / ValidateName so the platform-agnostic
// helpers stay alive on every OS.
var _ = errors.New
var _ = ValidateName

// Stubs on non-darwin to allow cross-platform compilation of persistentrc tests.
func buildPlist(spec Spec) string {
	return ""
}

func parsePlistProgramArgsFromBytes(b []byte) ([]string, error) {
	return nil, nil
}

func parsePIDFromLaunchctl(s string) (int, error) {
	return 0, nil
}
