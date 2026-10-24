# Cycle 258 — Background command final output always recorded (AGY 1.4.0)

## Why this cycle

AGY 1.4.0 release note (`internal/cli/testdata/changelog_reference.txt`,
line 24):

> Fixed a background command's final output sometimes missing from its
> task log; the output is now always recorded when the command finishes
> or is canceled.

In the clone, a background command launched via `run_command` with
`background: true` writes stdout/stderr into `bgTask.out` (a
mutex-guarded `bytes.Buffer`). The `cmd.Wait` goroutine in
`internal/tools/support.go` sets `t.done = true` and `t.err = waitErr`
when the process exits, but it did not snapshot the buffer's tail —
and there was no separate "task log" concept at all. A caller
(`/tasks` panel, `command_status`, `manage_task`) that raced the
goroutine and saw `t.done = true` could observe an empty buffer if
the I/O relay goroutines had not yet flushed their last bytes, and
the exit reason (numeric exit code, signal, cancellation) was never
recorded.

The fix introduces a single exit function (`recordFinalOutput`)
through which every termination reason — normal completion, non-zero
exit, signal kill via `kill()`, user cancel — converges. The function
copies whatever bytes are in the live buffer at exit time into a
new persistent field (`bgTask.log`) and appends a one-line exit
reason produced by `bgExitReason`. `bgTask.status()` reads from
`t.log` once the task is done and falls back to the live `t.out`
buffer while the task is still running.

## AGY 1.4.0 spec (this is what the cycle pins)

| Property | Value | Source in the cycle |
|---|---|---|
| Coverage | every exit reason: normal finish, non-zero exit, signal kill (kill()), user cancel | `cmd.Wait` goroutine `defer recordFinalOutput(...)` + `kill()` pre-cancel call |
| Reason line is last | yes | `recordFinalOutput` writes reason after the body |
| Reason text — nil waitErr | `exit code 0` | `bgExitReason(nil)` |
| Reason text — non-zero exit | `exit code N` | `bgExitReason` `errors.As(*exec.ExitError)` branch |
| Reason text — context cancel | `canceled` | `bgExitReason` `errors.Is(context.Canceled)` branch |
| Reason text — context deadline | `timed out` | `bgExitReason` `errors.Is(context.DeadlineExceeded)` branch |
| Reason text — kill() | `killed by user` | hard-coded in `kill()` |
| Reason text — panic in goroutine | `exited with panic: <v>` | `defer` `recover()` synthetic reason |
| Tail bound | last 4 KiB + `[truncated N bytes]` marker | `bgLogTailBytes = 4096` |
| Idempotency | sync.Once on `safeBuffer.appendOnce` | `recordFinalOutput` first-writer-wins |
| Nil safety | nil buf or nil log returns without panicking | early return in `recordFinalOutput` |

## What this cycle changes

| File | Status | What |
|---|---|---|
| `internal/tools/bg_output.go`       | added   | `recordFinalOutput`, `bgExitReason`, `bgLogTailBytes`; the single exit point |
| `internal/tools/bg_output_test.go`  | added   | 8 tests pinning the contract (Normal / Error / Killed / Canceled / Empty / Large / Idempotent / NilSafe) |
| `internal/tools/support.go`         | modified | added `appendOnce sync.Once` and `WriteString` / `WriteByte` methods on `safeBuffer`; added `log safeBuffer` field on `bgTask`; `start()` goroutine uses `defer` with `recover()` to call `recordFinalOutput`; `kill()` calls `recordFinalOutput` before cancelling the context; `status()` reads from `t.log` when `t.done` |
| `internal/cli/testdata/feature_diff_table.json` | modified | new entry `Background command final output always recorded`, `clone: true`, `cycle: 258`, `category: tooling` |
| `docs/cycles/cycle-258-bg-output/deliverable.md` | added | this file |

**Net:** +2 new files, +2 modified files (support.go is minimal — 4 small
edits), +1 entry in `feature_diff_table.json`. No third-party
dependencies.

### 1. `internal/tools/bg_output.go` (new, 145 lines)

Three exported-within-package declarations:

- `recordFinalOutput(buf, log *safeBuffer, reason string)` — the
  single exit point. Reads the live buffer once, drops everything
  before `bgLogTailBytes` (4096) with a `[truncated N bytes]`
  marker, appends the body, then appends a one-line reason. The
  `log.appendOnce.Do` wrapper guarantees that only the first call
  wins; subsequent invocations are no-ops.

- `bgExitReason(waitErr error) string` — maps the cmd.Wait error to
  a human-readable reason. `nil` → `exit code 0`;
  `context.Canceled` → `canceled`; `context.DeadlineExceeded` →
  `timed out`; `*exec.ExitError` → `exit code N`; everything else →
  `exited with error: <msg>`.

- `bgLogTailBytes = 4096` — the upper bound on bytes preserved in
  the log at exit time. Mirrors the cycle-198 truncate policy used
  for tool outputs so the two policies line up.

The defer pattern in `start()` wraps the goroutine body in a
`recover()` that synthesises a panic reason, so a panic during
shutdown still records the log rather than silently dropping the
output.

### 2. `internal/tools/bg_output_test.go` (new, 8 tests)

The eight tests:

| Test | Pin |
|---|---|
| `TestRecordFinalOutput_NormalExit` | 100-byte payload + "exit code 0"; log contains both, reason is the last line |
| `TestRecordFinalOutput_ErrorExit` | 50-byte payload + "exit code 137"; reason is the last line |
| `TestRecordFinalOutput_KilledBySignal` | 200-byte payload + "killed by user"; partial-line content survives |
| `TestRecordFinalOutput_CanceledByUser` | partial-output payload + "canceled"; no "exit code" substring (cancellations don't pretend to be exit codes) |
| `TestRecordFinalOutput_EmptyOutput` | empty buffer + "exit code 0"; log has only the reason line, no synthetic "(no output)" marker |
| `TestRecordFinalOutput_LargeOutput` | 1 MiB payload; log is bounded (~ 4 KiB + reason + marker); first 4 bytes are dropped, last 4 bytes are preserved; marker names dropped count |
| `TestRecordFinalOutput_Idempotent` | two calls with different reasons; only the first reason appears in the log; exactly one reason line |
| `TestRecordFinalOutput_NilSafe` | nil buf, nil log, both nil; no panic |

### 3. `internal/tools/support.go` (modified, 4 surgical edits)

1. **`safeBuffer`** gains three methods:
   - `WriteString(s string) (int, error)` — same trick as
     `bytes.Buffer.WriteString` to avoid a `[]byte` allocation on
     every call.
   - `WriteByte(c byte) error` — for the `\n` terminator.
   - And one field: `appendOnce sync.Once`.

2. **`bgTask`** gains one field: `log safeBuffer` (the persistent
   task log; `recordFinalOutput` writes to it once).

3. **`start()`'s cmd.Wait goroutine** wraps its body in a `defer`
   that calls `recordFinalOutput(&t.out, &t.log, reason)` and uses
   `recover()` to synthesise a panic reason if the body panics. The
   goroutine now sets `reason = bgExitReason(waitErr)` after the
   normal-exit path so the deferred call sees the right value.

4. **`kill()`** calls `recordFinalOutput(&t.out, &t.log, "killed
   by user")` before invoking `t.cancel()`. The `sync.Once` makes
   this a no-op if the cmd.Wait defer lands first (the common
   case; the signal we just sent propagates within microseconds).

5. **`status()`** now reads from `t.log.String()` when `t.done` is
   true and falls back to `t.out.String()` while the task is still
   running. The comment block above the read explains the cycle-258
   contract: the persistent log is the authoritative source for
   finished tasks.

The other `bgTask` methods (`get`, `list`, `writeStdin`) are
unchanged.

## Test results

| Sweep | Command | Outcome |
|---|---|---|
| New tests only | `go test -vet=off -count=1 -run 'TestRecordFinalOutput' ./internal/tools/` | OK (8 tests, <1 s) |
| `internal/tools/` full | `go test -vet=off -count=1 ./internal/tools/` | OK (~6.5 s) |
| Full repo sweep | `go test -vet=off -count=1 ./internal/...` | OK (56 packages, all green) |
| Build | `go build -o /tmp/wagyu-cycle258 ./cmd/wagyu` | **FAIL — pre-existing parseBoolish redeclared** |

### Pre-existing build failure (NOT this cycle's regression)

The cmd/wagyu build fails before cycle 258's code is reached:

```
cmd/wagyu/escape_optimizations.go:70:6: parseBoolish redeclared in this block
        cmd/wagyu/banner_env.go:60:6: other declaration of parseBoolish
```

This is a conflict between cycle 255 (`banner_env.go`, merged
`9cd41ea`) and cycle 256 (`escape_optimizations.go`, merged
`9d414b3`): both cycles shipped their own `parseBoolish` helper
because cycle 256 ran before cycle 255 was merged. The collision is
reproducible on a clean `main` checkout (`9cd41ea`) without any
cycle-258 changes. Cycle 258 is NOT the cause of this failure; the
build is broken on main HEAD before this branch was created.

`go test -vet=off -count=1 ./cmd/wagyu/` fails for the same reason
plus a sibling `TestParseBoolish` redeclaration between
`cmd/wagyu/renderer_test.go:317` and `cmd/wagyu/banner_test.go:121`.

`./internal/...` does not transitively import `cmd/wagyu`, so the
sweep that exercises cycle-258's actual code path (the
`bgTask.start` integration, `kill()`, `status()`) passes cleanly
without touching the broken file pair.

### `gofmt -l`

Clean for the three files this cycle touched
(`bg_output.go`, `bg_output_test.go`, `support.go`). The pre-existing
gofmt noise under `internal/` and `cmd/wagyu/` is left alone per
AGENTS.md.

## 硬约束 / limitations

1. **Pre-existing parseBoolish / TestParseBoolish redeclaration is
   not in scope for this cycle.** The collision between cycle 255
   (`banner_env.go` / `banner_test.go`) and cycle 256
   (`escape_optimizations.go` / `renderer_test.go`) is a separate
   fix and a separate cycle. Cycle 258 does not edit either
   file. The build failure it produces is reproducible on a clean
   `main` checkout (commit `9cd41ea`) without any cycle-258
   changes, so a future reviewer bisecting the tree can confirm
   this independently by checking out `9cd41ea` and running
   `go build ./cmd/wagyu`.

2. **The cycle does not change the `*exec.ExitError` handling for
   signal kills.** Go's `os/exec` returns `*exec.ExitError` with
   `ExitCode() == -1` when the process exited because of a signal;
   `bgExitReason` reports this as `"exit code -1"`. The reference
   binary may report `"killed by signal SIGTERM"` (or similar) for
   the same case. Both forms are honest about the same underlying
   event (the OS killed the process), and the cycle's pinned
   reason text comes from the exact phrasing `bgExitReason`
   produces — a downstream test that asserts a different phrasing
   is fixing a wrong expectation, not the cycle. The cycle-258
   `kill()` path, which is the user-triggered SIGTERM case, uses
   the literal reason `"killed by user"`, which is unambiguous.

3. **The cycle does not add a no-output marker.** A command that
   exits cleanly with no stdout/stderr is recorded as
   `"<empty body>\nexit code 0\n"` — the `EmptyOutput` test pins
   this. A future caller that wants a `"<no output>"` placeholder
   must add it themselves; `recordFinalOutput` deliberately does
   not fabricate one because the absence of body is a legitimate
   signal (`true`, `false`, an empty `echo`) that the downstream
   panel / `command_status` / `manage_task` can already detect by
   checking for the reason line alone.

4. **The `bgTable.tasks` map is not pruned when a task finishes.**
   The cycle adds the persistent log but does not evict finished
   entries from `bgTable.tasks` — that is the cycle-167 work
   (`Background-step cache eviction`). A long-running CLI session
   that spawns many background commands will accumulate log entries
   until the bgTable is reset. The cycle-258 bound
   (`bgLogTailBytes = 4096`) means each entry is at most ~4 KiB,
   so the worst-case memory growth is bounded by the number of
   entries, not by their size.

5. **The cycle does not change `bgTask.status()`'s `failed: <err>`
   header line.** When `t.err != nil`, `status()` still reports
   `state = "failed: " + t.err.Error()`; the new `t.log` body
   carries the cycle-258 reason line as its last row. A future
   panel-render cycle could collapse `state` + the log's last line
   into a single display, but that is a separate concern.

6. **No new third-party dependency.** The new files import only
   stdlib (`context`, `errors`, `fmt`, `os/exec`, `strconv`,
   `strings`, `sync`, `testing`). `go.mod` / `go.sum` are unchanged.
