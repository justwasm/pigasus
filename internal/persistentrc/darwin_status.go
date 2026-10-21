//go:build darwin

// darwin_status.go: the Status / StatusReport construction paths
// for the macOS / launchd implementation.
//
// Split out of darwin.go in cycle `rc-filesize-internal-persistentrc`
// so the 87-line `Status` orchestrator could be broken into smaller
// helpers, all ≤ 50 LOC.  The behaviour is byte-identical to the
// pre-cycle `Status` body:
//
//   - Status          — validate + dispatch to prod / dry-run
//   - statusFromLaunchctl — production path (reads launchctl print)
//   - statusFromDryRun    — test-only path (reads pidfile + plist)
//   - statusRunning       — shared "build a populated StatusReport"
//   - statusNotRegistered — shared "no LaunchAgent registered" stub
//   - statusHintLaunchedButStopped — "registered but not running"
//   - addrFromPlist        — extract --addr from the plist XML
//
// All helpers are private (lowercase) and consumed only by Status
// itself; the package's public surface is unchanged.

package persistentrc

import (
	"context"
	"os"
	"time"
)

// Status returns the daemon's state.  Always returns a populated
// StatusReport; the error is reserved for context cancellation
// or unrecoverable I/O failures (e.g. PlistDir unwriteable).
//
// The dry-run mode (Spec.SkipLaunchd) reads the pidfile that
// Start wrote and synthesizes the rest of the report from the
// plist + spec.
//
// Production mode runs `launchctl print gui/<uid>/<label>`,
// parses the PID, and constructs the rest of the report from
// the plist's ProgramArguments (addr / name) and the launchd
// print's "state = running" line.
func Status(ctx context.Context, name string) (StatusReport, error) {
	if err := ValidateName(name); err != nil {
		return StatusReport{}, err
	}
	// Resolve the canonical plist path so we can read
	// ProgramArguments back out of it (addr, etc.) when
	// launchctl is unavailable.
	plistP := PlistPathFor(name)
	if plistP == "" {
		return statusNotRegistered(name), nil
	}
	if !isDryRun() {
		return statusFromLaunchctl(name, plistP)
	}
	return statusFromDryRun(name, plistP), nil
}

// statusFromLaunchctl is the production Status path: ask launchd
// and translate its output into a StatusReport.  Idempotent and
// always returns a populated StatusReport (a populated body on
// success, a Hint-only stub on failure).
func statusFromLaunchctl(name, plistP string) (StatusReport, error) {
	pid, err := readLaunchdPrint(name)
	if err == nil && pid > 0 {
		return statusRunning(name, pid, plistP, startedAtFromPlist(plistP), true), nil
	}
	// launchctl failed → either no agent registered,
	// or the agent was registered but isn't running.
	// Fall through to the "no LaunchAgent" report.
	if _, statErr := os.Stat(plistP); statErr != nil {
		return statusNotRegistered(name), nil
	}
	return statusHintLaunchedButStopped(name), nil
}

// statusFromDryRun is the test-only Status path: read the
// pidfile Start wrote and synthesise the rest of the report
// from the plist.  running flag is computed via pidAlive so a
// follow-up check after the test process exits reports running=false.
func statusFromDryRun(name, plistP string) StatusReport {
	pid, _ := readPidfile(Spec{Name: name})
	if pid <= 0 {
		return statusNotRegistered(name)
	}
	// Compute uptime from the pidfile mtime when the
	// caller didn't pin Spec.Now.  Tests set Spec.Now via
	// the foreground handler's spec — but dry-run mode
	// bypasses that, so we use file mtime as the source of
	// truth for "started_at" in dry-run reports.
	startedAtEpoch := startedAtFromPlist(plistP)
	if startedAtEpoch == 0 {
		startedAtEpoch = nowFromMtime(plistP)
	}
	return statusRunning(name, pid, plistP, startedAtEpoch, pidAlive(pid))
}

// statusRunning builds the populated StatusReport body shared
// by the production and dry-run paths.  The caller passes the
// already-resolved startedAtEpoch so each path can compute it
// differently (production always uses startedAtFromPlist's
// mtime fallback; dry-run has an additional explicit mtime
// fallback on top of that).
func statusRunning(name string, pid int, plistP string, startedAtEpoch int64, running bool) StatusReport {
	now := Now()
	uptime := ""
	if startedAtEpoch > 0 && now > startedAtEpoch {
		uptime = formatUptime(time.Duration(now-startedAtEpoch) * time.Second)
	}
	return StatusReport{
		Running:        running,
		PID:            pid,
		Name:           name,
		Addr:           addrFromPlist(plistP),
		StartedAt:      formatTimestamp(startedAtEpoch),
		Uptime:         uptime,
		StartedAtEpoch: startedAtEpoch,
	}
}

// addrFromPlist reads the plist and returns the value of the
// --addr program argument.  Empty string when the plist can't
// be read or has no --addr element.
func addrFromPlist(plistP string) string {
	args, _ := parsePlistProgramArgs(plistP)
	for i, a := range args {
		if a == "--addr" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// statusNotRegistered is the canonical "no LaunchAgent
// registered; run `wagyu remote-control start`" stub.  Used by
// both production and dry-run paths when the plist is missing.
func statusNotRegistered(name string) StatusReport {
	return StatusReport{
		Name: name,
		Hint: "no LaunchAgent registered; run `wagyu remote-control start`",
	}
}

// statusHintLaunchedButStopped is the "LaunchAgent registered
// but not running; try stop then start" hint.  Used when the
// plist is on disk but launchctl reports no PID.
func statusHintLaunchedButStopped(name string) StatusReport {
	return StatusReport{
		Name: name,
		Hint: "LaunchAgent registered but not running; try `wagyu remote-control stop` then `start`",
	}
}
