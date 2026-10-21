//go:build darwin

// darwin_launchctl.go: launchctl invocation + print-output
// parsing for the macOS / launchd implementation.
//
// Split out of darwin.go in cycle `rc-filesize-internal-persistentrc`
// to keep the file below the 500-LOC ceiling.  This file owns:
//
//   - launchctlUID          — the gui/<uid> domain UID getter
//   - bootstrapLaunchd      — `launchctl bootstrap gui/<uid> <plist>`
//   - bootoutLaunchd        — `launchctl bootout gui/<uid>/<label>`
//   - readLaunchdPrint      — `launchctl print gui/<uid>/<label>`
//   - pidLineRE + parsePIDFromLaunchctl — pid regex + parser
//   - launchctlRunner + defaultLaunchctlRunner — exec.Command seam
//   - scanLines             — line scanner used by the test rig
//   - procSignal            — signal-number helper (currently unused
//                             but kept for symmetry with other ports)
//
// launchctl integration uses only `os/exec.Command("launchctl", …)`
// — no launchctl library, no mach RPC.  The output of `launchctl
// print` is text and easy enough to regex-parse; the surface we
// care about is just the PID.

package persistentrc

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// launchctlUID is the UID launchctl uses for the gui/<uid>
// domain.  Exposed as a var so tests can pin it.
var launchctlUID = func() int { return os.Getuid() }

// bootstrapLaunchd runs `launchctl bootstrap gui/<uid> <plist>`.
// When Spec.SkipLaunchd is true the call is a no-op (the plist
// + pidfile have already been written).
func bootstrapLaunchd(spec Spec) error {
	if spec.SkipLaunchd {
		return nil
	}
	uid := launchctlUID()
	cmd := exec.Command("launchctl", "bootstrap",
		fmt.Sprintf("gui/%d", uid),
		plistPath(spec))
	out, err := cmd.CombinedOutput()
	if err != nil {
		// launchctl's exit-1 with "service already
		// bootstrapped" is non-fatal — the agent may
		// already be running from a prior session.  Treat
		// that as success so Start is idempotent.
		if strings.Contains(string(out), "already") {
			return nil
		}
		return fmt.Errorf("launchctl bootstrap %s %s: %w (out=%q)",
			fmt.Sprintf("gui/%d", uid), plistPath(spec), err, string(out))
	}
	return nil
}

// bootoutLaunchd runs `launchctl bootout gui/<uid>/<label>`.
// Idempotent — a "service not bootstrapped" exit is treated as
// success so Stop never errors on a half-cleaned-up tree.
func bootoutLaunchd(name string) error {
	if pidfilePath(Spec{Name: name}) != "" {
		// SkipLaunchd path: leave the pidfile to the
		// caller's Stop path which removes the plist.
		_ = pidfilePath
	}
	uid := launchctlUID()
	cmd := exec.Command("launchctl", "bootout",
		fmt.Sprintf("gui/%d/%s", uid, Label(name)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "not found") ||
			strings.Contains(string(out), "not bootstrapped") {
			return nil
		}
		// Bootout can also fail with "no such process" /
		// "service not registered".  Treat all of those
		// as success so Stop is idempotent.
		if strings.Contains(string(out), "no such") {
			return nil
		}
		return fmt.Errorf("launchctl bootout: %w (out=%q)", err, string(out))
	}
	return nil
}

// readLaunchdPrint runs `launchctl print gui/<uid>/<label>` and
// parses the resulting text for a "pid = N" line.  Returns the
// PID on success; 0 + a non-nil error otherwise.  The launchctl
// print format is human-readable; the PID line has a leading
// space and looks like "	pid = 12345".  We regex that out.
func readLaunchdPrint(name string) (int, error) {
	uid := launchctlUID()
	cmd := exec.Command("launchctl", "print",
		fmt.Sprintf("gui/%d/%s", uid, Label(name)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("launchctl print: %w (out=%q)", err, string(out))
	}
	return parsePIDFromLaunchctl(string(out))
}

// pidLineRE matches a "pid = 12345" line in `launchctl print`
// output.  Leading whitespace + literal "pid" + optional space +
// "=" + optional space + digits.  Anchored so we don't match
// partial lines like "last exit code = 1" (which has a number but
// isn't the PID).
var pidLineRE = regexp.MustCompile(`(?m)^\s*pid\s*=\s*(\d+)\s*$`)

// parsePIDFromLaunchctl pulls the first PID-shaped line from a
// `launchctl print` blob.  Exposed for the test rig so the
// "fake print output" path can be unit-tested without spawning
// launchd.
func parsePIDFromLaunchctl(s string) (int, error) {
	m := pidLineRE.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("no pid line in launchctl print output")
	}
	pid, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, fmt.Errorf("malformed pid %q: %w", m[1], err)
	}
	return pid, nil
}

// launchctlRunner is exposed as a var so tests can swap in a fake
// launchctl printer.  Default is exec.Command.
var launchctlRunner = defaultLaunchctlRunner

// defaultLaunchctlRunner is the production path: exec the
// launchctl binary and capture combined output.
func defaultLaunchctlRunner(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s %v: %w (stdout=%q stderr=%q)",
			name, args, err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

// scanLines splits text into lines (no allocation when the
// scanner has a line limit we don't hit).  Exposed for the
// test rig so it can assert on a single fake-launchctl output
// line at a time.
func scanLines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// unixSignalTERM / KILL are the platform-specific signal
// numbers.  Defined here so the rest of the file stays
// signal-number-agnostic; if we ever need to send a different
// signal we add another const.
const (
	unixSignalTERM = 0xF // SIGTERM on darwin + linux
	unixSignalKILL = 0x9 // SIGKILL on darwin + linux
)

// procSignal sends a signal to a process; signal 0 is the
// "is alive?" probe.  We use syscall.Kill directly so we can
// pass an arbitrary signal number (os.FindProcess.Signal maps
// onto os.Interrupt / os.Kill only, which aren't portable).
func procSignal(pid, sig int) error {
	return syscall.Kill(pid, syscall.Signal(sig))
}
