# Cycle `rc-persistent-daemon` — `wagyu remote-control start|status|stop` + hidden `remote-control-foreground`

## TL;DR / 摘要

Cycles 1-10 of `exp/rc-server-mode` shipped the
JSON-RPC transport (`rc-ws-transport` `46b348b`),
the cascade lifecycle (`rc-cascade-start` /
`rc-cascade-message` / `rc-cascade-stream` /
`rc-handle-user-interaction`), the four read-only
catalog probes (`rc-list-readonly` `c5cc4af`), the
two read-only filesystem probes
(`rc-file-ops` `2fb7fbb`), the three project-CRUD
methods (`rc-projects` `f301fc1`), and the three
destructive delete methods
(`rc-conversation-deletions` `0ea3470`).

This cycle ships the **persistent-daemon lifecycle**
so the `remote-control start|status|stop` surface
that was already wired through `parser_specs.go`
moves from "stub that prints `not signed in`" to a
real launchd-backed daemon:

| Subcommand | Behaviour |
|---|---|
| `wagyu remote-control start` | validates `--addr/--log-file/--name/--session`; resolves `--name` to `os.Hostname()` and `--log-file` to `$TMPDIR/openagy-remote-control-<n>.log` when unset; writes the LaunchAgent plist (or session-detaches the foreground process for `--session`); bootstrap → 200 ms sleep → `launchctl print` PID probe → `Daemon started; pid=<pid>; addr=<addr>` |
| `wagyu remote-control status` | reads `launchctl print gui/$UID/<label>`; emits a JSON document: `{running, pid, name, addr, started_at, uptime, started_at_epoch, hint}` |
| `wagyu remote-control stop` | `launchctl bootout`, removes plist, SIGTERM with SIGKILL fallback, removes pidfile; idempotent — missing agent exits 0 |
| `wagyu remote-control-foreground` (hidden) | the long-running daemon the plist invokes; builds the dispatcher + TCPServer, blocks on SIGINT/SIGTERM, shuts down with a 5 s cap |

| Piece | Where |
|---|---|
| `Spec` / `StatusReport` / `ErrUnsupported` / `ErrInvalidAddr` / `ErrInvalidName` / `ResolveSpec` / `DefaultAddr` | `internal/persistentrc/persistentrc.go` (~229 lines) |
| `labelPrefix` / `Label` / `DefaultLaunchAgentDir` / `PlistPathFor` / `LogFilePath` / `ValidateName` / `ValidateAddr` / `Now` / `formatUptime` / `formatTimestamp` / `plistDirOverride` / `SetPlistDirOverride` | `internal/persistentrc/helpers.go` (~225 lines) |
| `buildPlist` / `writePlist` / `launchctlUID` / `bootstrapLaunchd` / `bootoutLaunchd` / `readLaunchdPrint` / `parsePIDFromLaunchctl` / `pidLineRE` / `readPidfile` / `removePidfile` / `Start` / `Stop` / `Status` / `parsePlistProgramArgs` / `unixSignalTERM` / `unixSignalKILL` / `procSignal` / `launchctlRunner` / `defaultLaunchctlRunner` / `isDryRun` / `SetDryRun` / `startedAtFromPlist` / `nowFromMtime` / `pidAlive` | `internal/persistentrc/darwin.go` (~721 lines) |
| non-darwin stubs returning `ErrUnsupported` from `Start`, no-op `Stop`, zero-value `Status` | `internal/persistentrc/other.go` (~114 lines) |
| 24 dry-run + plist tests (plist round-trip, session-flag, start/stop dry-run, status not-running, status running, label-format, validate-addr / validate-name matrix, signal constants) | `internal/persistentrc/persistentrc_test.go` (~554 lines) |
| `runRCStart` / `runRCStatus` / `runRCStop` real implementations | `internal/cli/subcommand_parity.go` (+302 / -68) |
| `usageRCStart` / `usageRCStatus` / `usageRCStop` updated with `--addr` / `--log-file` / `--json` / `--name` | `internal/cli/subcommand_parity.go` (+33 / -18) |
| `foregroundHandler` var + `RegisterForegroundHandler` setter + `runRCForegroundPlaceholder` | `internal/cli/foreground_bridge.go` (new, ~78 lines) |
| Hidden `remote-control-foreground` subcommand spec | `internal/cli/parser_specs.go` (+15 / -0) |
| `runRemoteControlForeground` (foreground daemon) + `redirectToLogFile` + `signalChProvider` / `signalChFromNotify` test seam | `cmd/wagyu/remote_control_foreground.go` (~250 lines) |
| `cli.RegisterForegroundHandler(runRemoteControlForeground)` wiring at startup | `cmd/wagyu/main.go` (+7 / -0) |
| 6 foreground-handler tests (missing-addr / missing-name / rejects-loopback / rejects-invalid-name / redirects-log-file / binds-and-shuts-down) | `cmd/wagyu/remote_control_foreground_test.go` (new, ~268 lines) |
| 8 subcommand-parity tests (start-help-only / stop-help-only / status-json / status-name-override / start-rejects-bogus / start-requires-arg-after-addr / foreground-hidden / foreground-direct-invocation / start-hostname-default) | `internal/cli/rc_persistent_daemon_test.go` (new, ~190 lines) |
| `TestRemoteControlStatus_1to1` rewritten to expect JSON document (running=false, name=hostname, hint=…) | `internal/cli/close_gap_test.go` (+24 / -9) |

Result: 24 (persistentrc) + 8 (subcommand_parity)
+ 6 (foreground) = **38 new tests**, all green.

What lands for forward consumers:

- An `internal/persistentrc/` lifecycle package
  future cycles can call directly to register /
  unregister / query the daemon without rebuilding
  the argv plumbing;
- A `StatusReport` JSON shape dashboards can rely
  on regardless of run state (the same fields
  appear populated or empty depending on whether
  the LaunchAgent is registered and the daemon
  alive);
- A hidden foreground subcommand with a
  cli-internal callback bridge (`RegisterForegroundHandler`),
  matching the chat bridge pattern from cycle 41
  — internal/cli/ stays import-cycle-free even
  though the handler body lives in cmd/wagyu/;
- A `Spec.SkipLaunchd=true` test mode that
  bypasses launchctl entirely; the whole test
  suite runs on Linux, macOS, and Windows without
  the service manager being available.

## Architecture

```
            ┌─────────────────────────────────────────┐
            │  user runs wagyu remote-control start   │
            │  → args parsed in subcommand_parity.go  │
            │    runRCStart  →  persistentrc.Start    │
            └──────────────────────┬──────────────────┘
                                   │
                  ┌────────────────┴────────────────┐
                  │  persistentrc.Start(ctx, spec)   │
                  │  ① ResolveSpec (defaults, valid) │
                  │  ② writePlist (~/Library/       │
                  │     LaunchAgents/com.openagy.   │
                  │     remote-control-<name>.plist)│
                  │  ③ bootstrapLaunchd             │
                  │      `launchctl bootstrap       │
                  │       gui/$UID <plist>`         │
                  │  ④ sleep 200 ms                 │
                  │  ⑤ readLaunchdPrint → parse pid │
                  │  └→ nil / *ErrPidUnresolved →   │
                  │     clean up via bootoutLaunchd │
                  │     + os.Remove(plist)          │
                  └────────────────┬────────────────┘
                                   │
                  launchd spawns (RunAtLoad):
                                   │
            ┌──────────────────────┴──────────────────┐
            │  wagyu remote-control-foreground       │
            │  --addr <addr> --name <n>              │
            │  --log-file <log>                      │
            │  → cli.RegisterForegroundHandler hook  │
            │    → cmd/wagyu/runRemoteControlForeground
            │      → buildHostFSProvider / ListBackend│
            │      → remotecontrol.NewDispatcher     │
            │      → remotetransport.NewTCPServer    │
            │      → signal.Notify(SIGINT/SIGTERM)   │
            │      → block on signal                 │
            │      → srv.Shutdown(5s ctx)            │
            └─────────────────────────────────────────┘
```

## Configuration surface

### `Spec` (`internal/persistentrc/persistentrc.go:55`)

| Field | Purpose |
|---|---|
| `Name` | LaunchAgent label suffix + log-file basename; required; conservative `[A-Za-z0-9._-]+` character set |
| `Addr` | Bind address for the foreground daemon's WebSocket listener; default `0.0.0.0:8787`; loopback refused |
| `LogFile` | Absolute path the plist's `StandardOutPath`/`StandardErrorPath` point at; default `$TMPDIR/openagy-remote-control-<n>.log` |
| `BinaryPath` | Absolute path to the wagyu binary the plist invokes; default `os.Executable()` |
| `Session` | `true` → `--session` mode; skp `RunAtLoad`+`KeepAlive`; spawn detached foreground instead of launchd plist |
| `SkipLaunchd` | `true` → test mode; write `<plist>.pid` + `<plist>.started_at` side-car files but skip `launchctl bootstrap` |
| `PlistDir` / `PlistPath` | Override the LaunchAgent directory + plist path; test-only |
| `Now func() int64` | Time source for `started_at`; test-only |

### `StatusReport` (`internal/persistentrc/persistentrc.go:132`)

```jsonc
{
  "running": true,
  "pid": 12345,
  "name": "gear.tailac191.ts.net",
  "addr": "0.0.0.0:8787",
  "started_at": "2026-10-19T12:34:56.789012345Z",
  "uptime": "1h23m",
  "started_at_epoch": 1729344896
}
```

Or when no agent is registered:

```json
{
  "running": false,
  "name": "gear.tailac191.ts.net",
  "hint": "no LaunchAgent registered; run `wagyu remote-control start`"
}
```

## Plist shape (`internal/persistentrc/darwin.go:78`)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.openagy.remote-control-<name></string>
  <key>ProgramArguments</key>
  <array>
    <string><abs-path-to-wagyu></string>
    <string>remote-control-foreground</string>
    <string>--addr</string>
    <string><addr></string>
    <string>--name</string>
    <string><name></string>
    <string>--log-file</string>
    <string><log-file></string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string><log-file></string>
  <key>StandardErrorPath</key>
  <string><log-file></string>
</dict>
</plist>
```

The `RunAtLoad`/`KeepAlive` pair is omitted when
`--session` is set; the foreground process is
session-scoped instead of boot-time.

The plist is byte-stable: identical inputs produce
byte-identical output. `TestBuildPlist_RoundTrip`
parses the rendered XML back through
`encoding/xml` (with a `Plist` helper struct) and
asserts the field shape. `TestBuildPlist_SessionFlag`
asserts `RunAtLoad`/`KeepAlive` are absent when
`Spec.Session=true`.

## Bridge pattern (`internal/cli/foreground_bridge.go`)

```
parser_specs.go:354  →  runRCForegroundPlaceholder
                              ↓ (if registered)
                       foregroundHandler (var)
                              ↑ set by
                       cli.RegisterForegroundHandler(...)
                              ↑
                       cmd/wagyu/main.go:273
                              ↑
                       runRemoteControlForeground
```

This is the same shape as the chat bridge
(`cli.SetChatBridge(runLiveTUI)` at
`cmd/wagyu/main.go:265`, defined in
`internal/cli/dispatch.go:737`). The internal/cli
package never imports cmd/wagyu/; the bridge
plumbing keeps the import cycle impossible.

When the bridge is unset (the parser-only test rig
in `internal/cli/`), `runRCForegroundPlaceholder`
prints a one-line stderr notice and exits 0;
`TestRemoteControlForeground_DirectInvocation`
exercises the bridge by installing a recording
handler.

## Test matrix

| Test | What it pins |
|---|---|
| `internal/persistentrc/ TestBuildPlist_RoundTrip` | The plist emits parseable XML with the expected field set |
| `internal/persistentrc/ TestBuildPlist_SessionFlag` | `RunAtLoad`/`KeepAlive` are absent when `Spec.Session=true` |
| `internal/persistentrc/ TestStartStop_DryRun` | Start+Stop in dry-run mode without touching launchctl |
| `internal/persistentrc/ TestStatus_NotRunning` | `Status` returns `Hint` set when no agent is registered |
| `internal/persistentrc/ TestStatus_RunningDryRun` | `Status` populates PID/addr/started_at/uptime in dry-run mode |
| `internal/persistentrc/ TestValidateAddr_*` | Loopback refused; link-local refused; `0.0.0.0`/`::` allowed; missing port; non-`:` form |
| `internal/persistentrc/ TestValidateName_*` | Empty rejected; spaces rejected; 200-char cap; conservative charset |
| `internal/persistentrc/ TestParsePlistProgramArgs_*` | Round-trips `--addr`/`--name`/`--log-file` through the renderer + parser |
| `internal/cli/ TestRemoteControlStart_HelpOnly` | `start --help` lists `--addr`/`--log-file`/`--name`/`--session` |
| `internal/cli/ TestRemoteControlStop_HelpOnly` | `stop --help` lists `--name` |
| `internal/cli/ TestRemoteControlStatus_JSONOutput` | `status` (no args) emits JSON starting with `{` and `running: false` |
| `internal/cli/ TestRemoteControlStatus_NameOverride` | `status --name X` propagates X through to the JSON document |
| `internal/cli/ TestRemoteControlStart_RejectsUnknownFlag` | `start --bogus` rejects with code 1 + flag name in stderr |
| `internal/cli/ TestRemoteControlStart_RequiresArgAfterAddr` | `start --addr` (no value) rejects |
| `internal/cli/ TestRemoteControlForeground_Hidden` | `--help` output does NOT mention `remote-control-foreground` |
| `internal/cli/ TestRemoteControlForeground_DirectInvocation` | The bridge forwards argv to the registered handler |
| `internal/cli/ TestRemoteControlStart_HostnameDefault` | `--name` resolves to `os.Hostname()` |
| `internal/cli/ TestRemoteControlStatus_1to1` (rewritten) | `status` exits 0, emits valid JSON document with `running=false` + `name=hostname` + `hint` |
| `cmd/wagyu/ TestForegroundHandler_MissingAddr` | The foreground handler rejects when `--addr` is absent |
| `cmd/wagyu/ TestForegroundHandler_MissingName` | Same for `--name` |
| `cmd/wagyu/ TestForegroundHandler_RejectsLoopbackAddr` | Loopback binds refused at the foreground layer (defence-in-depth) |
| `cmd/wagyu/ TestForegroundHandler_RejectsInvalidName` | Invalid `--name` characters rejected |
| `cmd/wagyu/ TestForegroundHandler_RedirectsLogFile` | `--log-file` Dup2's the file onto stdout/stderr |
| `cmd/wagyu/ TestForegroundHandler_BindsAndShutsDown` | Real bring-up: ephemeral port, dial successful, deterministic close on `signalChProvider` channel |

Total: 24 + 8 + 6 = **38 new tests**, all green.

## Run commands

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# All cycle tests
go test -race -count=1 -v -run 'TestBuildPlist_|TestStartStop_DryRun|TestStatus_|TestParsePlistProgramArgs|TestValidateAddr_|TestValidateName_|TestForegroundHandler_|TestRemoteControl' \
    ./internal/persistentrc/ ./internal/cli/ ./cmd/wagyu/

# Build the binary and verify start --help
go build -o /tmp/wagyu ./cmd/wagyu
/tmp/wagyu remote-control start --help

# status emits JSON (will print Hint because no
# LaunchAgent is registered in this checkout's
# development environment)
unset TMPDIR PERSISTENTRC_TEST_DRYRUN
/tmp/wagyu remote-control status

# Confirm the hidden subcommand is hidden
/tmp/wagyu --help 2>&1 | grep -c remote-control-foreground
# expected: 0
```

## Reviewer commands

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Confirm the cycle commit hash
git log --oneline -1
# expected: <this-commit's hash> first

# Show the cycle's file diff (stat)
git diff HEAD~1 HEAD --stat

# Run the cycle's in-process + parser tests
go test -race -count=1 -short \
    ./internal/persistentrc/ ./internal/cli/ ./cmd/wagyu/

# Confirm the persistentrc package is stdlib-only
go list -f '{{.Imports}}' ./internal/persistentrc/
# expected: no github.com/... entries

# Confirm the LaunchAgent label has the openagy prefix
grep -n "com.openagy.remote-control" internal/persistentrc/helpers.go
```

## Hard constraints / 硬约束 / limitations

- **`go test ./...` is not run.** The shipped
  command in `docs/cycles/PLAN.md` is scoped to
  `./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/ ./internal/persistentrc/ ./internal/cli/`.  The
  `cmd/wagyu/` surface ships pre-existing vet
  warnings on unrelated test files
  (`model_default_effort_test.go:67`) and
  pre-existing data-race failures on
  `TestSlashCompactWithInstructions`
  (`cmd/wagyu/slash_compact.go:454`); both are
  unchanged in this cycle and are excluded from
  the verification scope by `-vet=off -race`
  matching.  No new vet warnings or races
  introduced by this cycle.
- **`internal/persistentrc/darwin.go` and the
  test rig are darwin-only.** `internal/persistentrc/other.go`
  returns `ErrUnsupported` on non-darwin platforms
  (linux, windows) when `Spec.SkipLaunchd=false`
  AND `Spec.Session=false` — the production
  path.  Tests using `Spec.SkipLaunchd=true`
  pass on all platforms because no `launchctl`
  invocation happens.  The foreground handler
  itself (in `cmd/wagyu/`) works on any platform;
  it's just that the launchd-managed lifecycle
  is macOS-only.
- **The hidden subcommand is registered as
  `Hidden: true`** in `parser_specs.go` so
  `wagyu --help` doesn't show it.  Direct
  invocation via `wagyu remote-control-foreground ...`
  still works because the jetski parser matches
  the explicit subcommand name; the hidden flag
  only suppresses the help listing, not the
  dispatch path.
- **`fmt.Fprintln(os.Stderr, …)` after
  `redirectToLogFile` lands in the log file
  only.**  The test
  `TestForegroundHandler_RedirectsLogFile` does
  best-effort fd cleanup before returning so
  subsequent tests in the same `go test` run
  see the original stderr; the test cleanup is
  defensive only — the production handler is
  expected to never return to the foreground
  foreground after redirect.
- **Persistentrc is stdlib-only**, mirroring the
  research-tool no-dep constraint from
  `out/two-prong-attack.md §9` — no third-party
  libraries for a service-manager wrapper.  The
  only os calls are `os/exec` (for `launchctl`)
  and `syscall.Kill` (for the SIGTERM/SIGKILL
  fallback on orphaned PIDs).  Cycle IDs are
  semantic; this one is `rc-persistent-daemon`,
  no collision with numbered cycles.
- **The reference binary's `remote-control`
  surface is launched into an `os.Args[0]` of
  `agy`**; this cycle wires the same handlers
  but under the `wagyu` argv0 the openagy build
  uses (verified by `os.Args[0]` flowing through
  the parser's `cli.Name` field).  No byte-level
  parity claim is made against agy for these
  three subcommands because the recovered stub
  was the only available reference and the
  launchd plist / daemon lifecycle is recoverable
  only from the cycle's own design decisions,
  not from the binary's recovered strings.
- **The CLI prints `Daemon started; pid=…;
  addr=…`** on `start` success — wrapper
  scripts can parse the PID without launching
  `launchctl print` themselves.  The format is
  not configurable; future cycles can add a
  `--json` flag to `start` to emit the same
  JSON shape `status` already produces.

## What the cycle did NOT change

- The in-process `--remote-control` flag (per-turn
  daemon via Unix socket) — that surface is
  untouched and continues to use
  `cmd/wagyu/remote_control.go`'s wiring.
- The in-process `--remote-control-tcp` flag
  (per-turn daemon via TCP+WebSocket) — also
  untouched.
- The four read-only RPC methods
  (`ListRemoteAgent*`) — recovered in
  `rc-list-readonly` and used unchanged by the
  foreground dispatcher the cycle wires up.
- Any third-party dependency — `coder/websocket`
  v1.8.15 stays at the same version, and
  `internal/persistentrc/` is stdlib-only.
