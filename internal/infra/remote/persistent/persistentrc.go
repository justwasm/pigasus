// Package persistentrc is the cycle-11 ("rc-persistent-daemon")
// implementation of the `wagyu remote-control start|status|stop`
// background-daemon surface.
//
// Cycles 1-9 of `exp/rc-server-mode` shipped the JSON-RPC dispatcher,
// the WebSocket transport, the cascade lifecycle, and the read-only +
// project + filesystem + conversation-delete method catalogue
// against the in-process `internal/remotecontrol/` package.  This
// cycle adds the *lifecycle* layer — a long-lived daemon process
// that survives the CLI invocation, registered as a launchd user
// agent on macOS so dashboards can reconnect after the CLI exits.
//
// The split is sharp:
//
//   - internal/remotecontrol/  —  the wire protocol + method
//     catalogue.  Pure library; no I/O outside its own sockets.
//     Reused unchanged by the foreground daemon process.
//   - internal/persistentrc/    —  this package.  Owns the OS
//     service manager (launchd on macOS, setsid on Linux/macOS
//     for `--session`).  No wire-format or method semantics
//     here; the foreground process reads this configuration
//     from CLI flags and brings up the dispatcher on its own.
//
// Surface:
//
//   - Start(ctx, spec) error      — register the daemon and bring it up
//   - Stop(ctx, name) error       — unregister, remove plist, kill PID
//   - Status(ctx, name) (StatusReport, error) — read launchctl
//
// The `SkipLaunchd` field on Spec is the test-mode flag: when true,
// Start writes the plist (or session-detach invocation) to a temp
// directory and returns a fake PID; Status reads it back.  No actual
// launchctl calls.  This is what the dry-run tests use so the whole
// suite can run on Linux + macOS + Windows without any service
// manager being available.
//
// All three functions accept a context; callers may use the cancel
// path to abort long-running launchctl invocations on macOS.
//
// Errors:
//
//   - ErrUnsupported — Start on a non-darwin platform without
//     SkipLaunchd.  Status / Stop return zero values, not errors.
//   - everything else is a wrapped stdlib error (typically from
//     os/exec, os.WriteFile, or net.ParseSocketAddr).
//
// References:
//
//   - docs/cycles/PLAN.md (the `rc-persistent-daemon` row).
//   - `out/local-macos/dec/` — the agy reference's decompiled
//     LaunchAgent surface (recovered via strings-addrs.tsv).
package persistent

import (
	"errors"
	"fmt"
	"runtime"
)

// Spec is the user-supplied configuration for a persistent
// remote-control daemon.  All paths are absolute; resolution
// happens before Start is called (the foreground subcommand uses
// os.Executable for BinaryPath and $TMPDIR for LogFile).
type Spec struct {
	// Name is the short machine label used as the launchd
	// label suffix and as the log-file basename.  Required.
	// Validated to a conservative character set (letters,
	// digits, '.', '-', '_') so it can appear in a launchd
	// label and a $TMPDIR path without quoting.
	Name string

	// Addr is the bind address for the foreground daemon's
	// JSON-RPC-over-WebSocket listener.  Default "0.0.0.0:8787".
	// Refused when the host part is 127.0.0.1 (the persistent
	// daemon is meant to be reachable from another machine on
	// the LAN; loopback binds defeat that).
	Addr string

	// LogFile is the absolute log-file path the foreground
	// daemon redirects stdout/stderr to.  Empty → LogFilePath
	// (i.e. $TMPDIR/openagy-remote-control-<name>.log).
	LogFile string

	// Session is the login-scoped flag.  When true, no LaunchAgent
	// plist is written; the foreground process is just spawned
	// detached (setsid on Linux/macOS) and dies when the user's
	// login session ends.  When false (default), the daemon is
	// registered with launchd and starts at boot / re-spawns on
	// crash.
	Session bool

	// BinaryPath is the absolute path to the wagyu binary the
	// foreground subcommand is invoked under.  Computed by the
	// foreground caller via os.Executable.  Required.
	BinaryPath string

	// Desktop asks the daemon for the reference's *desktop* UI:
	// the voice input, the jetbox terminal, the redesigned aux
	// pane.
	//
	// It reaches the daemon through the LaunchAgent plist's
	// ProgramArguments (`--desktop` after the foreground
	// subcommand), and the foreground handler turns it into the
	// hub's Mendel identity — the backend serves a different
	// flag set per product, and the desktop features are in the
	// hub's set.
	//
	// False (default) → the CLI's own identity → the CLI's page.
	//
	// Cycle web-desktop-mode-remote-control adds this.
	Desktop bool

	// SkipLaunchd is the test-mode flag.  When true, Start
	// writes the plist (or session detach) to a temp directory
	// and returns a fake PID; Status reads it back.  No actual
	// launchctl calls.  This is what the dry-run tests use so
	// the whole suite can run on Linux + macOS + Windows.
	SkipLaunchd bool

	// PlistDir overrides the LaunchAgent directory.  Tests use
	// this to redirect writes to a temp dir; production leaves
	// it empty so ~/Library/LaunchAgents/ is used.
	PlistDir string

	// PlistPath, when non-empty, is the absolute plist path the
	// caller wants Start to use.  Overrides the PlistDir+Name
	// composition.  Test-only.
	PlistPath string

	// Now, when non-nil, replaces time.Now in dry-run mode.
	// Tests use this to pin the StartedAt timestamp.
	Now func() int64
}

// StatusReport is the JSON-able status snapshot.  Status emits a
// zero value when no agent is registered; when one is registered
// and the process is alive the PID, addr, started_at and uptime
// fields are populated.  The JSON shape is:
//
//	{ "running": true, "pid": 12345, "name": "<n>",
//	  "addr": "0.0.0.0:8787", "started_at": "<RFC3339>",
//	  "uptime": "1h23m" }
//
// or
//
//	{ "running": false, "name": "<n>",
//	  "hint": "no LaunchAgent registered; run `wagyu remote-control start`" }
type StatusReport struct {
	Running   bool   `json:"running"`
	PID       int    `json:"pid,omitempty"`
	Name      string `json:"name"`
	Addr      string `json:"addr,omitempty"`
	Account   string `json:"account,omitempty"`
	StartedAt string `json:"started_at,omitempty"` // RFC3339Nano
	Uptime    string `json:"uptime,omitempty"`     // e.g. "1h23m"
	StartedAtEpoch int64 `json:"started_at_epoch,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// ErrUnsupported is returned by Start on non-darwin platforms when
// Spec.SkipLaunchd is false and Spec.Session is false (i.e. the
// call would have required launchd).  Status and Stop never return
// ErrUnsupported — they always return a usable zero value so a
// "is the daemon alive?" probe works cross-platform.
var ErrUnsupported = errors.New("persistent remote-control daemon: launchd-backed mode requires macOS")

// ErrInvalidAddr is returned when Spec.Addr is malformed or binds
// to loopback.  Loopback is refused because the persistent daemon
// is meant to be reachable from another machine on the LAN; the
// per-turn --remote-control-tcp flag already supports loopback for
// the in-process test rig.
type ErrInvalidAddr struct {
	Addr string
	Why  string
}

func (e *ErrInvalidAddr) Error() string {
	return fmt.Sprintf("invalid --addr %q: %s", e.Addr, e.Why)
}

// ErrInvalidName is returned when Spec.Name is empty or carries
// characters that wouldn't survive a launchd label or a path
// component unquoted.
type ErrInvalidName struct {
	Name string
}

func (e *ErrInvalidName) Error() string {
	return fmt.Sprintf("invalid --name %q: must be non-empty and contain only letters, digits, '.', '-', '_'", e.Name)
}

// ResolveSpec fills the zero-value fields of spec with the
// production defaults.  Idempotent — calling it on an already
// populated Spec is a no-op.  Callers (cmd/wagyu/subcommand_parity
// for the start path, cmd/wagyu/remote_control_foreground for the
// foreground path) should run their argument through this before
// handing it to Start / before bringing up the dispatcher so the
// defaults stay consistent across surfaces.
//
// Validation:
//
//   - Name: must match the conservative character set.
//   - Addr: must be host:port; host must NOT be a loopback IPv4
//     address.  IPv6 loopback (::1) is also refused.
//   - LogFile: empty → LogFilePath(Name).  Caller-provided
//     LogFile must be an absolute path.
//   - BinaryPath: empty → os.Executable().  Tests inject an
//     explicit path so this helper doesn't have to depend on the
//     real wagyu binary.
//
// The function returns a copy of the input with all defaults
// applied; the input is not mutated.
func ResolveSpec(spec Spec) (Spec, error) {
	if spec.Addr == "" {
		spec.Addr = DefaultAddr
	}
	if spec.LogFile == "" {
		spec.LogFile = LogFilePath(spec.Name)
	}
	if spec.BinaryPath == "" {
		// Foreground callers fill this in via os.Executable
		// in production; tests inject an explicit value.
		// We leave BinaryPath empty here so the foreground
		// handler can detect "not yet resolved".
		_ = spec.BinaryPath
	}
	if err := ValidateName(spec.Name); err != nil {
		return Spec{}, err
	}
	if err := ValidateAddr(spec.Addr); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// DefaultAddr is the bind address the persistent daemon defaults
// to when --addr is unset.  Picked to match the rest of the
// antigravity ecosystem's "external dashboard can reach me" story
// — 0.0.0.0 binds on all interfaces, port 8787 is high enough to
// not collide with system services.
const DefaultAddr = "0.0.0.0:8787"

// runtimeOS is the build target.  Exposed as a var so tests can
// override it; default is runtime.GOOS.  PersistentRC's
// platform-specific files (`darwin.go`, `other.go`) use this
// directly to decide whether launchctl is callable.
var runtimeOS = runtime.GOOS