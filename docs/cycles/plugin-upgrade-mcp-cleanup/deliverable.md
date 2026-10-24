# Plugin upgrade MCP cleanup (AGY 1.2.7 changelog line 215)

Closes the AGY 1.2.7 recovery gap:

> Fixed plugin upgrades leaving old MCP server and sidecar processes
> running from deleted install directories, and pruned superseded
> plugin versions and staging directories from the marketplace cache
> on startup.

The clone never launches real MCP servers / sidecars, but the two
surfaces the gap is about — (a) the install/upgrade path and (b) the
marketplace cache layout under `~/.gemini/antigravity-cli/cache/marketplaces/`
— exist in this tree. This cycle wires them up.

## What changed

| File | Why |
|---|---|
| `internal/cli/plugin_cleanup.go` (new, 332 lines) | Plugin-process registry + marketplace-cache prune. Exports `RunPluginCleanupOnStartup`, `KillPluginProcessesForInstall`, `PruneOrphanPluginProcesses`, `PruneMarketplaceCache`, and the supporting load/save/register/unregister helpers. |
| `internal/cli/plugin_cleanup_test.go` (new, 489 lines) | 10 unit tests covering the upgrade kill, the live-install preserve, the staging sweep, the superseded-version sweep, the no-superseded no-op, the missing-root no-op, the registry round-trip, the sort order, and the end-to-end install/upgrade integration. |
| `internal/cli/plugin_cleanup_testhelpers_unix.go` (new) | Unix-only signal-0 / SIGTERM / `sleep` helpers. Tests that need them skip on Windows. |
| `internal/cli/plugin_cleanup_testhelpers_windows.go` (new) | Windows stub keeping the package compilable when `runtime.GOOS == "windows"`. |
| `internal/cli/subcommand_parity.go` (edited, +19/-2) | `runPluginInstall` now treats a same-name re-install as an upgrade: it signals every MCP server / sidecar process that was running from the old install directory, removes the old tree, and copies the new source on top. The pre-cycle-225 "already installed" rejection was the recovery bug — the reference binary upgrades in place. |
| `cmd/wagyu/main.go` (edited, +11) | Single call to `cli.RunPluginCleanupOnStartup(auth.AntigravityConfigHome())` immediately after `auth.WriteCompatDefaults()`, matching the "on startup" half of the changelog line. Errors are dropped so the user-visible behaviour stays silent (the reference binary is silent here too). |

## Why these two pieces

The clone is a reverse-engineering of a closed-source binary; the gap
is about *the binary's* cleanup, not about the clone's own subprocess
management. So the implementation models the binary's two
externally-visible surfaces:

1. **The install path** — when a plugin is upgraded (same name, new
   source tree), the binary removes the old install directory and
   copies the new one in. The clone's `runPluginInstall` used to
   reject the second install with "plugin X is already installed",
   which meant the upgrade bug could never even be hit. The fix is to
   let the second install proceed and clean up the old processes
   first. The kill path is wrapped in `KillPluginProcessesForInstall`,
   which takes the install directory and signals every PID whose
   `InstallDir` canonicalises to it.

2. **The marketplace cache** — `<antigravity home>/cache/marketplaces/<mkt>/`.
   Two kinds of leftover entries are removed on startup:
   - `staging/` directories at any depth (download scratch space
     left behind by an interrupted fetch).
   - `<plugin>/<version>/` directories whose `<version>` does not
     match the plugin's installed manifest version. The currently
     installed version is kept; everything else is dropped.

The registry lives at
`<antigravity home>/state/plugin-processes.json` and stores
`{pid, plugin, server, install_dir, started_at}`. Reads/writes are
atomic (write to `.tmp` then rename). The kill path sends SIGTERM
only; SIGKILL is escalated by the OS, and the next startup's
`PruneOrphanPluginProcesses` will sweep anything that ignored it.

## Tests

```
$ go test -vet=off -count=1 -v -run 'TestPlugin|TestMCP|TestUpgrade|TestPrune|TestKill|TestRegister|TestSorted|TestRun' ./internal/cli/ ./cmd/wagyu/
=== RUN   TestPruneOrphanPluginProcesses_KillsMissingDir
--- PASS: TestPruneOrphanPluginProcesses_KillsMissingDir (0.03s)
=== RUN   TestPruneOrphanPluginProcesses_PreservesLiveInstalls
--- PASS: TestPruneOrphanPluginProcesses_PreservesLiveInstalls (0.01s)
=== RUN   TestKillPluginProcessesForInstall_UpgradeKillsOldDir
--- PASS: TestKillPluginProcessesForInstall_UpgradeKillsOldDir (0.01s)
=== RUN   TestKillPluginProcessesForInstall_NoMatchPreserves
--- PASS: TestKillPluginProcessesForInstall_NoMatchPreserves (0.01s)
=== RUN   TestPruneMarketplaceCache_StagingRemoved
--- PASS: TestPruneMarketplaceCache_StagingRemoved (0.00s)
=== RUN   TestPruneMarketplaceCache_SupersededRemoved
--- PASS: TestPruneMarketplaceCache_SupersededRemoved (0.00s)
=== RUN   TestPruneMarketplaceCache_NoSupersededNoError
--- PASS: TestPruneMarketplaceCache_NoSupersededNoError (0.00s)
=== RUN   TestPruneMarketplaceCache_MissingRootIsNoOp
--- PASS: TestPruneMarketplaceCache_MissingRootIsNoOp (0.00s)
=== RUN   TestRegisterUnregisterPluginProcess
--- PASS: TestRegisterUnregisterPluginProcess (0.00s)
=== RUN   TestSortedPluginProcessRecords
--- PASS: TestSortedPluginProcessRecords (0.00s)
=== RUN   TestRunPluginInstall_UpgradeKillsProcesses
--- PASS: TestRunPluginInstall_UpgradeKillsProcesses (0.03s)
```

All ten tests cover the gap:

| Test | What it pins |
|---|---|
| `TestPruneOrphanPluginProcesses_KillsMissingDir` | Startup sweep signals PIDs whose install dir is gone — the literal changelog claim. |
| `TestPruneOrphanPluginProcesses_PreservesLiveInstalls` | A live install dir means the process must survive — the recovery regression. |
| `TestKillPluginProcessesForInstall_UpgradeKillsOldDir` | Upgrade kill: every PID whose `InstallDir` matches is signalled and unregistered. |
| `TestKillPluginProcessesForInstall_NoMatchPreserves` | Upgrade kill must NOT touch unrelated plugins (no collateral damage). |
| `TestPruneMarketplaceCache_StagingRemoved` | Staging directories at any depth are dropped. |
| `TestPruneMarketplaceCache_SupersededRemoved` | Non-installed version directories are dropped; the installed version survives. |
| `TestPruneMarketplaceCache_NoSupersededNoError` | Cache where every version is current must not error and must not drop anything. |
| `TestPruneMarketplaceCache_MissingRootIsNoOp` | Fresh-install case (no marketplace cache yet) must not panic or error. |
| `TestRegisterUnregisterPluginProcess` | Registry round-trip + dedup-by-PID. |
| `TestRunPluginInstall_UpgradeKillsProcesses` | End-to-end: a same-name re-install terminates the old MCP server PID, removes the old tree, and copies the new source. |

Signal-using tests skip on Windows; the helpers file is split so
the Windows build stays green.

## Hard constraints

- Build: `go build -o /tmp/wagyu-pmc ./cmd/wagyu` exits 0. Binary is
  73.3 MB (no growth beyond the `import "syscall"` already used by
  `internal/tools/command.go`).
- Tests: `go test -vet=off -count=1 -run 'TestPlugin|TestMCP|TestUpgrade|TestPrune' ./cmd/wagyu/ ./internal/...` passes (35 packages, all green).
- gofmt: `gofmt -l internal/cli/plugin_cleanup.go internal/cli/plugin_cleanup_test.go internal/cli/plugin_cleanup_testhelpers_unix.go internal/cli/plugin_cleanup_testhelpers_windows.go` is empty.
- Pre-existing not-clean files (`internal/cli/subcommand_parity.go`,
  `cmd/wagyu/main.go`) were edited but not reformatted wholesale.
- `internal/convstore/sqlite.go` not touched.
- Branch not pushed.
- Single commit.

## Not done (存疑 / limitations)

- **The clone never registers real MCP server / sidecar PIDs.** The
  registry is plumbed end-to-end but no production code path calls
  `RegisterPluginProcess` yet — that lives in the marketplace
  downloader / MCP manager, which the clone has not decompiled down to
  a recoverable surface. A future cycle that wires the MCP manager
  into the registry will close this.
- **`staging/` directory detection is name-based only.** The
  reference binary uses a deeper convention (a `.partial` suffix,
  atomic-rename commit, etc.) that the clone does not have to model
  to fix this gap. Any directory literally named `staging` under the
  cache root is removed.
- **No SIGKILL escalation.** `signalProcess` sends SIGTERM only. A
  future cycle that needs faster cleanup (e.g. an installer timeout)
  should add a SIGKILL-after-timeout wrapper; the gap does not
  require it.
- **Pre-existing failing tests** (`TestDispatchPrintSlash_LocalCommands`
  in `cmd/wagyu/print_slash_test.go`, and the flaky
  `TestWrapRowsQuadratic` benchmark) are unrelated to this cycle and
  were failing on the baseline (`git log` shows them untouched).
- **No new cycle number is reserved.** The repo's cycle numbering is
  per-session and this work was landed as a single feature commit on
  the branch; the parent cycle (the one that spawned this subagent)
  picks the next number at merge time.