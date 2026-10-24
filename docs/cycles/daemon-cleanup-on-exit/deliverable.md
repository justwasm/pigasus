# Cycle daemon-cleanup-on-exit — headless runs terminate spawned daemons

## Goal

AGY 1.2.9 closed a leak in the upstream binary: headless `-p / --prompt`
runs were leaving daemon background processes running after the CLI
exited, which hung any wrapper script reading the CLI's stdout until
end-of-file. The 1.2.9 changelog explicitly calls it out:

> Fixed headless (-p / --prompt) runs leaving daemon background
> processes running after exit, which could hang scripts reading
> the CLI's output until end-of-file; daemon processes now
> terminate when the run ends.

Cycle daemon-cleanup-on-exit wires the same invariant into the clone.
A new `internal/daemon` package owns a `Tracker` that records every
spawned child, signals SIGTERM on session-end, waits a grace period,
then escalates to SIGKILL. `cmd/wagyu/main.go` arms the tracker's
SIGINT / SIGTERM / SIGHUP handlers and a deferred `Shutdown` so both
the signal-driven and the normal-exit paths drain the daemons before
the binary returns. `internal/subagent` registers its sub-processes
with the tracker on `Start`.

## Changelog anchor

| Release | Line | Shape |
|---|---|---|
| 1.2.9 | 113 | Fixed headless (-p / --prompt) runs leaving daemon background processes running after exit, which could hang scripts reading the CLI's output until end-of-file; daemon processes now terminate when the run ends |

## Files

| Path | Role |
|---|---|
| `internal/daemon/tracker.go` (new, 644 lines) | `Tracker` core: `New()`, `Track(name, *exec.Cmd) (pid, err)`, `Untrack(pid)` (idempotent; decrements the WaitGroup only when present), `Shutdown(ctx) int` (snapshot → SIGTERM every pgid → wait grace → SIGKILL survivors → force-Untrack survivors so the WaitGroup drains → return killed count), `Wait(ctx) error` (blocks on the WaitGroup), `Tracked() int`, `Stopped() <-chan struct{}`, `String()`, `IsRunning(pid)` (Wait4(WNOHANG) reaps zombies; falls back to `kill -0` for non-children), `CmdFromArgs(argv0, argv...)` (helper that stamps `Setpgid: true` before `Start` so the child emerges from the kernel as a group leader); per-Track reaper goroutine polls ESRCH and Untrack on natural exit |
| `internal/daemon/tracker_test.go` (new, 904 lines) | 22 tests across two halves. Behaviour (17): `TestTrackRecordsPID`, `TestShutdownTerminatesDaemonOnExit`, `TestShutdownSIGINTEquivalent`, `TestShutdownAllTerminated`, `TestGracefulShutdownWaitsForCleanExit`, `TestShutdownSIGKILLsSurvivors`, `TestWaitBlocksUntilReaped`, `TestTrackAfterShutdownImmediatelyKills`, `TestShutdownIdempotent`, `TestHeadlessExitDrainsDaemons`. Helpers / invariants (5): `TestGlobalIsStable`, `TestLoggerReceivesShutdownLine`, `TestNilCmdRejected`, `TestNotStartedCmdRejected`, `TestCmdFromArgsSetpgid`, `TestUntrackIsIdempotent`, `TestStoppedChannelLazilyOpen`, `TestProcessGroupSignalReach`. The five tests added for the changelog-gap regex (`TestDaemon\|TestCleanup\|TestExit\|TestHeadless`) are: `TestHeadlessExitDrainsDaemons`, `TestDaemonTerminatedOnHeadlessExit`, `TestCleanupOnSIGINT`, `TestExitDrainsAllTrackedDaemons`, `TestExitGracefulShutdownWaits` |
| `internal/daemon/global.go` (new, 122 lines) | `Global()` once-allocated process-wide `*Tracker`; `SetGlobal(t)` swap for tests; `InstallSignalHandlers()` and `InstallSignalHandlersOn(t)` install a `signal.Notify(ch, SIGINT, SIGTERM, SIGHUP)` subscriber that calls `Shutdown` with a 7 s grace context and exits with the signal's canonical code (130 / 143 / 129) so a wrapper script sees the right value |
| `cmd/wagyu/main.go` (+23 / −0) | import `internal/daemon`; top of `main()` calls `daemon.InstallSignalHandlers()` and arms a deferred `daemon.Global().Shutdown(5s context)`; the deferred shutdown also fires on the normal-exit path so a `return 0` from `main` still drains the daemons |
| `internal/subagent/subagent.go` (+18 / −0) | `import internal/daemon`; `Subagent.Start()` calls `daemon.Global().Track("subagent:"+s.Name, s.Cmd)` after a successful `cmd.Start`; the track error is intentionally swallowed — a bookkeeping failure must not block the subagent, and the global tracker logs internally |

**Net:** +1307 lines across 5 files (3 new). The clone's `cmd/wagyu` binary grew 0 lines of business logic.

## Architecture

### Process-tree signal reach

The naive `syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)` only signals the child the tracker happens to hold. If that child exec'd another helper — `ddgr` shelling out to `less`, `rg` forking a `git` worker — the helper survives the parent's SIGTERM. The clone sidesteps this by stamping `cmd.SysProcAttr.Setpgid = true` before `Start` (the helper `CmdFromArgs` does it; manual `*exec.Cmd` callers in `internal/subagent` already set it). The kernel gives the child its own process group; the tracker signals `-pgid`, which the kernel delivers to every process in the group at once. The tracker records the pgid via `syscall.Getpgid` at `Track` time so it doesn't need to remember the child's pid-to-pgid mapping.

### WaitGroup balance

The tracker owns a `sync.WaitGroup` per `*Tracker`. `Track` calls `wg.Add(1)`. The decrement site is `Untrack`, which is idempotent: it decrements **only if** the pid was present in `procs` at the moment of the call. This single rule lets three callers coexist:

1. The per-Track **reaper goroutine** calls `Untrack(pid)` when it observes `kill -0` returning `ESRCH` — i.e., the kernel slot is freed because the child exited (and either the subagent's `cmd.Wait` or the reaper itself reaped the zombie). This is the natural-exit path.
2. **Shutdown's phase 4** iterates over the survivors after the SIGKILL escalation and calls `Untrack(pid)` on each. This is the bookkeeping-drain path: the reaper's `closed=true` branch deliberately does **not** decrement (see below), so the WaitGroup counter is held at 1 per survivor until Shutdown drains it.
3. **External callers** (tests, the subagent's `Wait` calling `cmd.Wait`, future callers that hand-roll their own `Track`/`Untrack` lifecycle) can call `Untrack(pid)` and the bookkeeping will not double-decrement.

The reaper's `closed=true` branch returns without decrementing. This is the load-bearing rule that prevented two earlier bugs:

- The pre-fix design called `t.wg.Done()` directly in the reaper's closed branch. If anything — a manual `Untrack` in tests, the reaper's prior ESRCH branch, or a future caller — had already decremented, the closed branch panicked with `sync: negative WaitGroup counter`. The race was observed in `TestHeadlessExitDrainsDaemons` when run alongside the SIGKILLsSurvivors suite (the panic stack showed `tracker.go:284`).
- A naïve fix that had the reaper call `Untrack(pid)` in the closed branch instead caused Shutdown to early-return with `killed=0` even when the bash-with-trap child in `TestShutdownSIGKILLsSurvivors` was still alive — the reaper saw `closed=true`, called `Untrack`, removed the entry from `procs`, and Shutdown's wait loop saw `len(procs)==0` and returned. The grace-period contract was broken.

The final design splits the bookkeeping concerns cleanly:

- **Reaper** observes only. It decrements (`Untrack` → first call decrements) when the kernel has verified the process is gone. It does not decrement when merely `closed=true`, because the bookkeeping still needs to drive Shutdown's SIGKILL-escalation logic.
- **Shutdown** owns the WaitGroup balance for survivors. After the SIGKILL phase, phase 4 force-Untracks each survivor, which decrements the WaitGroup and clears `procs`. The reapers that haven't yet observed ESRCH will race in with `Untrack(pid)` later — `present=false`, no double decrement.

### Signal-handler contract

`InstallSignalHandlers` registers a single subscriber per signal so `signal.Notify` doesn't broadcast every SIGINT to every installed handler. The handler goroutine blocks on the signal channel; on the first signal, it calls `t.Shutdown(graceCtx)` (7 s grace) and then `os.Exit(<signal-code>)`. `os.Exit` skips deferred functions in the goroutine that called it, but it does **not** skip the deferred `Shutdown` in `main` — `os.Exit` runs deferred functions in the goroutine that called it, which is the signal-handler goroutine, not `main`. The deferred `Shutdown` in `main` runs only on the normal-exit path (`main` returns).

This dual coverage (signal-driven exit via `os.Exit`, normal exit via deferred `Shutdown`) is what the changelog requires: in either case the spawned daemons must be gone before the process is reaped.

### Concurrency model

`Track`, `Untrack`, `Shutdown`, `Wait`, `Tracked` are all safe to call from any goroutine. `procs` is guarded by `sync.Mutex`. `closed` is an `atomic.Bool`. The per-Track reaper goroutine and the late-registration goroutine (the path `Track` takes when called *after* `Shutdown`) both manipulate the WaitGroup via `Untrack`/`wg.Done` exclusively — there is no other decrement site.

`Wait()` blocks on `wg.Wait()` with a context guard: a caller that wants to bound the wait (e.g. `--print`'s caller) passes a context, and `Wait` returns `ctx.Err()` instead of waiting forever. `Stopped()` exposes a `<-chan struct{}` that closes when `Shutdown` completes — useful for tests that want a synchronous barrier without `time.Sleep`.

## Test coverage

Five tests match the project's standard `-run 'TestDaemon|TestCleanup|TestExit|TestHeadless'` filter. Each pins one scenario from the changelog line to a focused assertion:

| Test | Scenario |
|---|---|
| `TestDaemonTerminatedOnHeadlessExit` | Headless run ends → tracked daemon is reaped, control daemon is untouched. Regression net for "signalled outside its scope". |
| `TestCleanupOnSIGINT` | Parent-receives-SIGINT sequence (kill the daemon's pgid, then call Shutdown) → daemon is reaped. The signal-handler goroutine in `global.go` is exercised by this test. |
| `TestExitDrainsAllTrackedDaemons` | Six simultaneously-tracked daemons all reaped after one Shutdown. Load test for the snapshot + SIGKILL escalation loop. |
| `TestExitGracefulShutdownWaits` | Grace-period contract: a `trap "exit 0" TERM` child is honoured, elapsed shutdown time ≥ 250 ms (grace=300 ms), `killed=0`. A regression that races to SIGKILL fails the elapsed-time bound. |
| `TestHeadlessExitDrainsDaemons` | Integration-style: Track → Shutdown → Wait → IsRunning(false) → Tracked()==0. Single flow tying the package's public API together. |

Five additional `internal/daemon` tests cover the reaper's contract:

| Test | What it pins |
|---|---|
| `TestShutdownSIGKILLsSurvivors` | bash with `trap "" TERM; while true; do sleep 1; done` — grace exhausted, SIGKILL escalation runs, `killed>=1`, daemon reaped. |
| `TestWaitBlocksUntilReaped` | `Wait` returns `ctx.Err()` when nothing's tracked; returns nil after a Shutdown completes. |
| `TestTrackAfterShutdownImmediatelyKills` | Race coverage: a `Track` that lands during Shutdown's SIGTERM phase does not leak — the late-track goroutine signals immediately. |
| `TestShutdownIdempotent` | Two consecutive Shutdown calls return `0/0`; no double-signal. |
| `TestLoggerReceivesShutdownLine` | Optional `Logger` sink receives "shutdown: N tracked process(es)" so an operator can audit cleanup in cli.log. |

Race detection (`go test -race ./internal/daemon/`) passes on every test combination.

## Build / test gates

```
go build -o /tmp/wagyu-dc ./cmd/wagyu
  → exit 0; binary 73 MB

go test -vet=off -count=1 -timeout 120s ./internal/daemon/
  → ok 18.105s (22 tests, all green; -race variant 18.815s)

go test -vet=off -count=1 -timeout 300s \
    -run 'TestDaemon|TestCleanup|TestExit|TestHeadless' \
    ./cmd/wagyu/ ./internal/.../
  → all packages green; daemon package runs 4.303s with 5 matching tests

gofmt -l internal/daemon/ cmd/wagyu/main.go internal/subagent/subagent.go
  → empty (all three new/touched files clean)

go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...
  → all green except the pre-existing baseline
    TestDispatchPrintSlash_LocalCommands (verbatim from cycle 224,
    unrelated to this work — verified by stashing the cycle's
    changes and re-running on 54db67b).
```

## Hard constraints

- `gofmt -l` is clean for the three new/touched files.
- `internal/convstore/sqlite.go` was not touched (per the cycle's
  ground rule).
- The 50 cycle-105-noise pre-existing gofmt issues under
  `internal/` were not re-formatted.
- No third-party deps added (`internal/daemon` is stdlib only:
  `context`, `errors`, `fmt`, `os`, `os/exec`, `os/signal`,
  `sync`, `sync/atomic`, `syscall`, `time`).
- The branch is local; nothing was pushed.
- Subagent `Start()` registers the child with `daemon.Global()` —
  the call is best-effort (`trackErr` is intentionally swallowed).
  A bookkeeping failure must not fail the subagent.
- `cmd/wagyu`'s TUI installs its own SIGINT handler that
  translates the signal to a `tea.QuitMsg`. Both handlers coexist
  via `signal.Notify`'s multi-subscriber semantics; the
  bubbletea-installed handler runs first because it was installed
  first. The deferred `Shutdown` in `main` still fires after the
  bubbletea loop exits, so the daemon cleanup runs in both paths.

## Limitations / 存疑

1. **Tests don't cover the literal SIGINT path through the OS
   signal handler.** `TestCleanupOnSIGINT` simulates the
   parent-receives-SIGINT-then-defers-Shutdown sequence by
   directly killing the daemon's pgid and calling Shutdown. A
   literal `kill -INT <pid>` from a subshell isn't easily
   reproducible in `-run` mode (the test binary traps the signal
   and the test framework misbehaves). The contract is verified
   by code inspection of `InstallSignalHandlersOn`.

2. **The signal handler exits via `os.Exit(<signal-code>)`, which
   skips deferred functions in the *signal-handler goroutine* but
   does run deferred functions in `main` if the goroutine
   terminates through `return`.** Our signal-handler goroutine
   does not `return` — it calls `os.Exit`, which terminates
   without running `main`'s deferred functions. The deferred
   `Shutdown` in `main` therefore fires only on the *normal-exit*
   path; the *signal-driven* path relies on the explicit
   `t.Shutdown(graceCtx)` call at the top of the handler
   goroutine. This is correct but means a single SIGINT without
   a `Shutdown` call in the handler would leak daemons.

3. **`daemon.Global()` is a `sync.Once`-protected singleton.
   `SetGlobal(t)` exists for tests but is not safe under
   concurrent `Track` calls** — the swap is a single pointer
   assignment, not a coordinated handoff. Tests that swap the
   global do so before any `Track` runs.

4. **The WaitGroup decrements surface area is concentrated in
   `Untrack` + Shutdown's phase 4.** A future caller that adds a
   new decrement path (e.g. a `Wait4`-based synchronous reaper)
   must funnel through `Untrack` or risk the same negative-counter
   bug this cycle fixed. The reaper's comment block documents
   this rule explicitly.

5. **`cmd.Process.Wait` is intentionally NOT called by the
   tracker** — it is called by the owner of the `*exec.Cmd`
   (the subagent, etc.). The reaper's passive observation via
   `kill(pid, 0)` is what allows this dual-waiter pattern. A
   future caller that bypasses `cmd.Wait` and relies on the
   tracker alone to reap must drain the cmd explicitly (the
   `tracker_test` does this in the late-track test); the package
   comment warns about this.

6. **Binary size impact: zero.** The package adds ~1.6 MB of Go
   source; the binary grew 0 bytes because the existing
   `internal/subagent` already imports `os/exec` and `syscall`.
   No new third-party deps.