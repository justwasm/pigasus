// helpers.go: shared utilities for the persistentrc package.
//
// This file holds the path / label / validation helpers that both
// `darwin.go` and `other.go` need but don't otherwise have a
// clean place for.  It deliberately does NOT contain any platform
// switches; the platform-specific files call these directly.

package persistentrc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// labelPrefix is the launchd-label prefix for the wagyu-managed
// LaunchAgent.  The full label is "<prefix><name>".  The brief
// chose "com.openagy.remote-control" rather than the reference
// binary's "com.antigravity.remote-control" to keep the openagy
// fork's launchd footprint distinct from the upstream.
const labelPrefix = "com.openagy.remote-control-"

// Label returns the launchd label for the given name.  Exposed
// for tests so the assertion matches the production wiring.
func Label(name string) string {
	return labelPrefix + name
}

// DefaultLaunchAgentDir is the canonical macOS location for
// per-user LaunchAgents.  Resolved at runtime (not via a hard-
// coded path) so a future Homebrew or sandbox layout that
// redirects $HOME keeps working.
func DefaultLaunchAgentDir() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents")
}

// PlistPathFor returns the LaunchAgent plist path for the given
// name using the default directory.  Empty string when $HOME is
// unset.  Tests override the active dir via SetPlistDirOverride.
func PlistPathFor(name string) string {
	dir := activePlistDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, Label(name)+".plist")
}

// activePlistDir is the resolver behind PlistPathFor and the
// darwin-side writePlist path.  Tests override the var via
// SetPlistDirOverride so the production Path (~/Library/LaunchAgents)
// is bypassed in favour of t.TempDir().
func activePlistDir() string {
	if plistDirOverride != "" {
		return plistDirOverride
	}
	return DefaultLaunchAgentDir()
}

// plistDirOverride is the test-only redirect for activePlistDir.
// SetPlistDirOverride flips it; production never touches it.
var plistDirOverride string

// SetPlistDirOverride is the test-only entry point that redirects
// the plist / pidfile path to a temp directory.  Production
// callers never invoke it.
func SetPlistDirOverride(dir string) {
	plistDirOverride = dir
}

// LogFilePath returns the default log file path for the given
// name.  $TMPDIR is preferred (matches the rest of the
// antigravity stack); falls back to os.TempDir when TMPDIR is
// unset.  Empty string when neither is reachable.
func LogFilePath(name string) string {
	dir := strings.TrimSpace(os.Getenv("TMPDIR"))
	if dir == "" {
		dir = os.TempDir()
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "openagy-remote-control-"+name+".log")
}

// validNameRe is the conservative character set for Spec.Name.
// Letters / digits / '.' / '-' / '_' are safe inside a launchd
// label (the launchd label grammar is more permissive but the
// conservative set keeps the path component unquoted).
var validNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateName returns an *ErrInvalidName when name is empty or
// carries characters outside the conservative set.  nil on
// success.
//
// Exported (rather than internal-only) because the foreground
// handler in cmd/wagyu/ runs without going through ResolveSpec
// — the launchd-managed process doesn't trust the caller to
// have validated the name already, so the foreground re-checks.
func ValidateName(name string) error {
	if name == "" {
		return &ErrInvalidName{Name: name}
	}
	if len(name) > 200 {
		return &ErrInvalidName{Name: name}
	}
	if !validNameRe.MatchString(name) {
		return &ErrInvalidName{Name: name}
	}
	return nil
}

// ValidateAddr returns an *ErrInvalidAddr when addr is malformed,
// binds to loopback, or is missing a port.  nil on success.
//
// Loopback is refused (both 127.0.0.0/8 and ::1) because the
// persistent daemon is meant to be reachable from another
// machine on the LAN.  The per-turn --remote-control-tcp flag
// already supports loopback for the in-process test rig; the
// persistent path deliberately does not.
//
// Exported (rather than internal-only) for the same reason as
// ValidateName.
func ValidateAddr(addr string) error {
	if addr == "" {
		return &ErrInvalidAddr{Addr: addr, Why: "empty address"}
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return &ErrInvalidAddr{Addr: addr, Why: err.Error()}
	}
	if host == "" {
		return &ErrInvalidAddr{Addr: addr, Why: "missing host"}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return &ErrInvalidAddr{Addr: addr, Why: "port must be 1..65535"}
	}
	// Loopback refusal.
	if host == "localhost" {
		return &ErrInvalidAddr{Addr: addr, Why: "loopback refused (use 0.0.0.0 or a LAN IP)"}
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return &ErrInvalidAddr{Addr: addr, Why: "loopback refused (use 0.0.0.0 or a LAN IP)"}
		}
		if ip.IsUnspecified() {
			// 0.0.0.0 / :: — explicit "all interfaces".  Allowed.
		}
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return &ErrInvalidAddr{Addr: addr, Why: "link-local addresses are not persistent-daemon-friendly"}
		}
	}
	return nil
}

// Now is the time-source the helpers below use when the caller
// didn't supply one on Spec.  Tests override spec.Now to pin
// timestamps.  Wrapping time.Now directly keeps the foreground
// daemon's "started_at" aligned with the launchd bootstrap.
func Now() int64 {
	return time.Now().Unix()
}

// resolveStartedAtEpoch is the helper that picks the timestamp
// the StatusReport / launchd print parse will populate
// "started_at" with.  Tests override spec.Now to pin the value.
// The function's contract is identical to Now(); spec is passed
// in so the helper can read spec.Now without an extra layer of
// indirection at every call site.
func resolveStartedAtEpoch(spec *Spec) int64 {
	if spec != nil && spec.Now != nil {
		// spec.Now is func() int64; the helper returns
		// time.Now().Unix() when unset.
		return spec.Now()
	}
	return Now()
}

// formatUptime renders a duration as the "1h23m" / "45s" / "1d2h"
// short-form the StatusReport surfaces.  Empty when d is zero or
// negative.  The format deliberately mirrors the bash `uptime`
// command's "1 user,  load average: …" column shape because that
// is what dashboards already parse.
func formatUptime(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	// Truncate to whole seconds; the seconds field is what the
	// reference exposes.
	totalSec := int64(d.Seconds())
	days := totalSec / 86400
	totalSec -= days * 86400
	hours := totalSec / 3600
	totalSec -= hours * 3600
	minutes := totalSec / 60
	seconds := totalSec - minutes*60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// formatTimestamp renders an epoch-second as an RFC3339Nano string
// in UTC.  Empty on zero.  Used for the StatusReport.StartedAt
// field; tests pin both the input and the expected output.
func formatTimestamp(epochSec int64) string {
	if epochSec == 0 {
		return ""
	}
	return time.Unix(epochSec, 0).UTC().Format(time.RFC3339Nano)
}